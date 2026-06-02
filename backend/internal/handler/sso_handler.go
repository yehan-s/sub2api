package handler

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	// 一次性授权码的有效期：足够完成一次浏览器重定向 + 后端换取
	ssoCodeTTL = 5 * time.Minute
	// SSO 临时 key 的有效期（会话级、可回收），不是 1 小时
	ssoTempKeyDays = 30
	// SSO 临时 key 的名称：每次登录重签，签新前先按此名回收旧的，避免在 api_keys 表堆积。
	ssoTempKeyName = "studio-sso"
	// Redis 中授权码的键前缀
	ssoCodeRedisPrefix = "sso:code:"
)

// SSOHandler 处理子产品（如画境工坊生图站）的单点登录。
// 流程：用户在 sub2api 已登录 -> /sso/authorize 用登录态换一次性 code 并重定向回子产品
//
//	-> 子产品后端用共享密钥调 /sso/token 换“临时 key + 用户信息”。
//
// 临时 key 复用普通 API Key（Quota=0 共享用户余额 + 30 天过期），不新建表、不改 schema。
type SSOHandler struct {
	apiKeyService *service.APIKeyService
	userService   *service.UserService
	redis         *redis.Client
	cfg           *config.Config
}

// ssoCodePayload 是一次性 code 在 Redis 里绑定的内容：用户 + 用户选定的生图分组（可空）。
type ssoCodePayload struct {
	UserID  int64  `json:"uid"`
	GroupID *int64 `json:"gid,omitempty"`
}

// NewSSOHandler 创建 SSOHandler
func NewSSOHandler(
	apiKeyService *service.APIKeyService,
	userService *service.UserService,
	redisClient *redis.Client,
	cfg *config.Config,
) *SSOHandler {
	return &SSOHandler{
		apiKeyService: apiKeyService,
		userService:   userService,
		redis:         redisClient,
		cfg:           cfg,
	}
}

// Authorize 处理 GET /api/v1/auth/sso/authorize（挂在 jwtAuth 后，复用现有登录态）。
// 校验 redirect_uri 白名单 -> 生成一次性 code 存 Redis（绑 user_id）-> 302 重定向回子产品。
func (h *SSOHandler) Authorize(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	if strings.TrimSpace(h.cfg.SSO.SharedSecret) == "" {
		response.Error(c, http.StatusServiceUnavailable, "SSO not configured")
		return
	}
	if h.redis == nil {
		response.InternalError(c, "SSO unavailable")
		return
	}

	redirectURI := strings.TrimSpace(c.Query("redirect_uri"))
	state := c.Query("state")
	if redirectURI == "" || !h.isAllowedRedirect(redirectURI) {
		response.BadRequest(c, "redirect_uri not allowed")
		return
	}

	// 可选：用户在中继页选定的生图分组。临时 key 会绑定到该分组，决定可用模型与计费。
	var groupID *int64
	if g := strings.TrimSpace(c.Query("group_id")); g != "" {
		parsed, parseErr := strconv.ParseInt(g, 10, 64)
		if parseErr != nil || parsed <= 0 {
			response.BadRequest(c, "invalid group_id")
			return
		}
		groupID = &parsed
	}

	code, err := randomSSOToken(32)
	if err != nil {
		response.InternalError(c, "generate code failed")
		return
	}
	codeValue, err := json.Marshal(ssoCodePayload{UserID: subject.UserID, GroupID: groupID})
	if err != nil {
		response.InternalError(c, "encode code failed")
		return
	}
	if err := h.redis.Set(c.Request.Context(), ssoCodeRedisPrefix+code, codeValue, ssoCodeTTL).Err(); err != nil {
		response.InternalError(c, "store code failed")
		return
	}

	// 拼接回跳地址，带上 code 与原样回传的 state
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	target := redirectURI + separator + "code=" + url.QueryEscape(code)
	if state != "" {
		target += "&state=" + url.QueryEscape(state)
	}
	// 返回 JSON 而非 302：sub2api 为 header 认证（JWT 在 Authorization 头），
	// 该接口只能被持有 token 的 SPA 用 fetch 调用，故由前端中继页拿到 redirect 后自行跳转。
	response.Success(c, gin.H{"redirect": target})
}

type ssoTokenRequest struct {
	Code   string `json:"code"`
	Secret string `json:"secret"`
}

// Token 处理 POST /api/v1/auth/sso/token（公开路由，靠共享密钥鉴权）。
// 验密钥 -> 一次性取出并删除 code -> 为该用户签临时 key -> 返回 {temp_key, user}。
func (h *SSOHandler) Token(c *gin.Context) {
	configured := strings.TrimSpace(h.cfg.SSO.SharedSecret)
	if configured == "" {
		response.Error(c, http.StatusServiceUnavailable, "SSO not configured")
		return
	}

	var req ssoTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 共享密钥：优先取请求头，其次取 body
	provided := req.Secret
	if headerSecret := c.GetHeader("X-SSO-Secret"); headerSecret != "" {
		provided = headerSecret
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(configured)) != 1 {
		response.Unauthorized(c, "invalid SSO secret")
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		response.BadRequest(c, "code required")
		return
	}
	if h.redis == nil {
		response.InternalError(c, "SSO unavailable")
		return
	}

	ctx := c.Request.Context()
	// 一次性消费：取出后立即删除，防重放
	raw, err := h.redis.GetDel(ctx, ssoCodeRedisPrefix+code).Result()
	if err != nil || raw == "" {
		response.Unauthorized(c, "invalid or expired code")
		return
	}
	var cp ssoCodePayload
	if err := json.Unmarshal([]byte(raw), &cp); err != nil || cp.UserID <= 0 {
		response.Unauthorized(c, "invalid code")
		return
	}
	userID := cp.UserID

	user, err := h.userService.GetByID(ctx, userID)
	if err != nil {
		response.Unauthorized(c, "user not found")
		return
	}

	// 签新临时 key 前，先回收该用户此前签发的临时 key。
	// 临时 key 每次登录都重签（绑定当次所选分组），旧的不回收会逐次堆积成孤儿。
	h.revokePreviousTempKeys(ctx, userID)

	days := ssoTempKeyDays
	// 临时 key 绑定用户选定的生图分组（GroupID 为空则用默认分组）。
	// Create 内部会校验该用户是否有权绑定该分组，无权则返回错误。
	apiKey, err := h.apiKeyService.Create(ctx, userID, service.CreateAPIKeyRequest{
		Name:          ssoTempKeyName,
		Quota:         0, // 0 = 共享用户余额，扣的是用户自己的额度
		ExpiresInDays: &days,
		GroupID:       cp.GroupID,
	})
	if err != nil {
		if response.ErrorFrom(c, err) {
			return
		}
		response.InternalError(c, "create temp key failed")
		return
	}

	response.Success(c, gin.H{
		"temp_key": apiKey.Key,
		"user": gin.H{
			"id":       user.ID,
			"email":    user.Email,
			"username": user.Username,
		},
	})
}

// revokePreviousTempKeys 回收该用户此前签发的 SSO 临时 key（名为 ssoTempKeyName）。
// 不阻断登录：清理是尽力而为，签发新 key 才是主流程，故忽略错误。
// 逐个走 service.Delete 而非批量删，是为了顺带清掉每把 key 的认证缓存。
// 分页循环兜底已被污染（历史堆积多把）的存量用户，每轮删一批直到清空。
func (h *SSOHandler) revokePreviousTempKeys(ctx context.Context, userID int64) {
	params := pagination.PaginationParams{Page: 1, PageSize: 1000, SortBy: "created_at", SortOrder: "asc"}
	for iter := 0; iter < 50; iter++ {
		// Search 命中 Name/Key 子串；下面再按精确 Name 过滤，避免误删用户自建的 key。
		keys, _, err := h.apiKeyService.List(ctx, userID, params, service.APIKeyListFilters{Search: ssoTempKeyName})
		if err != nil {
			return
		}
		deleted := 0
		for i := range keys {
			if keys[i].Name != ssoTempKeyName {
				continue
			}
			if err := h.apiKeyService.Delete(ctx, keys[i].ID, userID); err == nil {
				deleted++
			}
		}
		// 本轮没删掉任何精确同名 key：要么已清空，要么剩下的都删不动，停止避免空转。
		if deleted == 0 {
			return
		}
	}
}

// isAllowedRedirect 校验回跳地址是否在白名单内（逗号分隔，前缀匹配到具体回调路径）。
func (h *SSOHandler) isAllowedRedirect(redirectURI string) bool {
	allow := strings.TrimSpace(h.cfg.SSO.AllowedRedirects)
	if allow == "" {
		return false
	}
	u, err := url.Parse(redirectURI)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	for _, item := range strings.Split(allow, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if redirectURI == item || strings.HasPrefix(redirectURI, item) {
			return true
		}
	}
	return false
}

func randomSSOToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
