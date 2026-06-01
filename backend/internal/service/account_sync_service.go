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
	accountRepo AccountRepository
	cfg         config.ResellerSyncConfig
}

// NewAccountSyncService 构造同步服务。
func NewAccountSyncService(accountRepo AccountRepository, cfg config.ResellerSyncConfig) *AccountSyncService {
	return &AccountSyncService{accountRepo: accountRepo, cfg: cfg}
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
