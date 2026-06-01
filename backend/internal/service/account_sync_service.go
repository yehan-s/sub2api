package service

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

// AccountSyncService 实现分销站「从生产同步账号」：只读连接生产库，把「生产有、分销没有」
// 的账号（含分组、代理）只新增到本地，已存在的（按 sync_source_id）跳过。
// 整套由 config.ResellerSyncConfig.Enabled 把闸。
type AccountSyncService struct {
	accountRepo    AccountRepository
	proxyRepo      ProxyRepository
	groupRepo      GroupRepository
	settingService *SettingService
	cfg            config.ResellerSyncConfig
}

// NewAccountSyncService 构造同步服务。
func NewAccountSyncService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	groupRepo GroupRepository,
	settingService *SettingService,
	cfg config.ResellerSyncConfig,
) *AccountSyncService {
	return &AccountSyncService{
		accountRepo:    accountRepo,
		proxyRepo:      proxyRepo,
		groupRepo:      groupRepo,
		settingService: settingService,
		cfg:            cfg,
	}
}

// SyncSourceAccount 从生产库读出的账号快照（仅含同步/预览所需信息）。
type SyncSourceAccount struct {
	ID         int64
	Name       string
	Platform   string
	Type       string
	GroupNames []string
	HasProxy   bool
}

// PreviewAccount 预览清单中的一条候选账号（生产有、分销没有）。
type PreviewAccount struct {
	SourceID int64    `json:"source_id"`
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	Type     string   `json:"type"`
	Groups   []string `json:"groups"`
	HasProxy bool     `json:"has_proxy"`
}

// PreviewResult 预览返回：可同步的候选账号列表。
type PreviewResult struct {
	Candidates []PreviewAccount `json:"candidates"`
}

// diffCandidates 返回「生产有、但本地尚未同步（按 sync_source_id）」的账号。
// 纯函数，便于单测。
func diffCandidates(prod []SyncSourceAccount, existing map[int64]bool) []SyncSourceAccount {
	out := make([]SyncSourceAccount, 0)
	for _, a := range prod {
		if !existing[a.ID] {
			out = append(out, a)
		}
	}
	return out
}

// Preview 只读连接生产库，列出「生产有、分销没有」的账号供前端勾选。
func (s *AccountSyncService) Preview(ctx context.Context) (*PreviewResult, error) {
	if !s.cfg.Enabled {
		return nil, fmt.Errorf("分销同步未启用")
	}
	srcClient, err := OpenSourceClient(s.cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = srcClient.Close() }()

	prod, err := readSourceAccounts(ctx, srcClient)
	if err != nil {
		return nil, fmt.Errorf("读取生产账号失败: %w", err)
	}
	existing, err := s.accountRepo.ListSyncedSourceIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取本地已同步集合失败: %w", err)
	}

	candidates := diffCandidates(prod, existing)
	result := &PreviewResult{Candidates: make([]PreviewAccount, 0, len(candidates))}
	for _, a := range candidates {
		result.Candidates = append(result.Candidates, PreviewAccount{
			SourceID: a.ID,
			Name:     a.Name,
			Platform: a.Platform,
			Type:     a.Type,
			Groups:   a.GroupNames,
			HasProxy: a.HasProxy,
		})
	}
	return result, nil
}

// readSourceAccounts 只读生产库的 accounts（含分组名与是否带代理）。
// 仅触达 sync_ro 授权的表：accounts、account_groups、groups（WithGroups 经此二者）。
// 绝不预加载会 join 到未授权表（如 usage_logs）的 edge。
func readSourceAccounts(ctx context.Context, client *dbent.Client) ([]SyncSourceAccount, error) {
	rows, err := client.Account.Query().
		Where(dbaccount.DeletedAtIsNil()).
		WithGroups().
		All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]SyncSourceAccount, 0, len(rows))
	for _, m := range rows {
		names := make([]string, 0, len(m.Edges.Groups))
		for _, g := range m.Edges.Groups {
			names = append(names, g.Name)
		}
		out = append(out, SyncSourceAccount{
			ID:         m.ID,
			Name:       m.Name,
			Platform:   m.Platform,
			Type:       m.Type,
			GroupNames: names,
			HasProxy:   m.ProxyID != nil,
		})
	}
	return out, nil
}

// ImportFailure 单个账号导入失败的记录（reason 为面向人的中文文本）。
type ImportFailure struct {
	SourceID int64  `json:"source_id"`
	Reason   string `json:"reason"`
}

// ImportResult 导入汇总。
type ImportResult struct {
	CreatedAccounts int             `json:"created_accounts"`
	CreatedGroups   int             `json:"created_groups"`
	CreatedProxies  int             `json:"created_proxies"`
	Skipped         int             `json:"skipped"`
	Failed          []ImportFailure `json:"failed"`
}

// Import 把选中的生产账号（含分组、代理）只新增到本地。
// 已存在的（按 sync_source_id）跳过；逐账号尽力而为，单个失败不影响其余。
func (s *AccountSyncService) Import(ctx context.Context, sourceIDs []int64) (*ImportResult, error) {
	if !s.cfg.Enabled {
		return nil, fmt.Errorf("分销同步未启用")
	}
	srcClient, err := OpenSourceClient(s.cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = srcClient.Close() }()

	existing, err := s.accountRepo.ListSyncedSourceIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取本地已同步集合失败: %w", err)
	}
	markup := s.settingService.GetSyncDefaultMarkup(ctx)

	result := &ImportResult{Failed: make([]ImportFailure, 0)}
	for _, sid := range sourceIDs {
		if existing[sid] {
			result.Skipped++
			continue
		}
		if err := s.importOne(ctx, srcClient, sid, markup, result); err != nil {
			result.Failed = append(result.Failed, ImportFailure{SourceID: sid, Reason: err.Error()})
			continue
		}
		result.CreatedAccounts++
	}
	return result, nil
}

// importOne 导入单个生产账号：解析代理 → 建账号(source=synced) → 解析/新建分组 → 绑定。
// 无显式事务（与现有 CRS 同步一致，尽力而为）；中途出错由调用方记入 failed。
func (s *AccountSyncService) importOne(ctx context.Context, src *dbent.Client, sid int64, markup float64, result *ImportResult) error {
	m, err := src.Account.Query().
		Where(dbaccount.IDEQ(sid), dbaccount.DeletedAtIsNil()).
		WithGroups().
		Only(ctx)
	if err != nil {
		return fmt.Errorf("读取生产账号失败: %w", err)
	}

	// 1) 解析代理：按连接身份元组匹配本地，缺则建
	var localProxyID *int64
	if m.ProxyID != nil {
		srcProxy, err := src.Proxy.Get(ctx, *m.ProxyID)
		if err != nil {
			return fmt.Errorf("读取生产代理失败: %w", err)
		}
		puser := derefStr(srcProxy.Username)
		ppass := derefStr(srcProxy.Password)
		found, err := s.accountRepo.FindProxyByIdentity(ctx, srcProxy.Protocol, srcProxy.Host, srcProxy.Port, puser)
		if err != nil {
			return fmt.Errorf("匹配本地代理失败: %w", err)
		}
		if found != nil {
			localProxyID = &found.ID
		} else {
			np := &Proxy{
				Name:     srcProxy.Name,
				Protocol: srcProxy.Protocol,
				Host:     srcProxy.Host,
				Port:     srcProxy.Port,
				Username: puser,
				Password: ppass,
				Status:   StatusActive,
			}
			if err := s.proxyRepo.Create(ctx, np); err != nil {
				return fmt.Errorf("新建代理失败: %w", err)
			}
			localProxyID = &np.ID
			result.CreatedProxies++
		}
	}

	// 2) 建账号（走会入队 outbox 的 Create）
	acct := &Account{
		Name:         m.Name,
		Platform:     m.Platform,
		Type:         m.Type,
		Credentials:  m.Credentials,
		Extra:        m.Extra,
		Concurrency:  m.Concurrency,
		Priority:     m.Priority,
		Status:       m.Status,
		Schedulable:  m.Schedulable,
		Source:       "synced",
		SyncSourceID: &sid,
		ProxyID:      localProxyID,
	}
	rm := m.RateMultiplier
	acct.RateMultiplier = &rm
	if err := s.accountRepo.Create(ctx, acct); err != nil {
		return fmt.Errorf("新建账号失败: %w", err)
	}

	// 3) 解析/新建分组（缺则按默认加价系数建），再绑定
	groupIDs := make([]int64, 0, len(m.Edges.Groups))
	for _, g := range m.Edges.Groups {
		local, err := s.accountRepo.FindGroupByName(ctx, g.Name)
		if err != nil {
			return fmt.Errorf("匹配本地分组失败: %w", err)
		}
		if local == nil {
			ng := &Group{
				Name:           g.Name,
				Platform:       g.Platform,
				RateMultiplier: markup,
				Status:         StatusActive,
			}
			if err := s.groupRepo.Create(ctx, ng); err != nil {
				return fmt.Errorf("新建分组失败: %w", err)
			}
			local = ng
			result.CreatedGroups++
		}
		groupIDs = append(groupIDs, local.ID)
	}
	if len(groupIDs) > 0 {
		if err := s.accountRepo.BindGroups(ctx, acct.ID, groupIDs); err != nil {
			return fmt.Errorf("绑定分组失败: %w", err)
		}
	}
	return nil
}
