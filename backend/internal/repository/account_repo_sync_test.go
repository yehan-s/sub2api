//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

// AccountRepoSyncSuite 验证分销同步所需的 repo 能力：
//  1. Create 能落 source/sync_source_id，且 scheduler_outbox 正确入队；
//  2. FindGroupByName：按名称匹配未软删除的分组；
//  3. FindProxyByIdentity：按连接身份元组匹配未软删除代理，多命中取最新。
type AccountRepoSyncSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	repo   *accountRepository
}

func (s *AccountRepoSyncSuite) SetupTest() {
	s.ctx = context.Background()
	tx := testEntTx(s.T())
	s.client = tx.Client()
	// sql executor 也用同一 tx，确保 scheduler_outbox 在事务内可见
	s.repo = newAccountRepositoryWithSQL(s.client, tx, nil)
}

func TestAccountRepoSyncSuite(t *testing.T) {
	suite.Run(t, new(AccountRepoSyncSuite))
}

// --- Create 落 source/sync_source_id + outbox 回归 ---

// TestCreate_WithSourceAndSyncSourceID 建账号时设置 source="synced"/sync_source_id，
// 验证两字段正确落库，且 scheduler_outbox 写入了 AccountChanged 行（outbox 回归）。
func (s *AccountRepoSyncSuite) TestCreate_WithSourceAndSyncSourceID() {
	syncSourceID := int64(9999)
	account := &service.Account{
		Name:         "sync-test-account",
		Platform:     service.PlatformAnthropic,
		Type:         service.AccountTypeOAuth,
		Status:       service.StatusActive,
		Credentials:  map[string]any{},
		Extra:        map[string]any{},
		Concurrency:  3,
		Priority:     50,
		Schedulable:  true,
		Source:       "synced",
		SyncSourceID: &syncSourceID,
	}

	// 清空 outbox，确保计数基准归零
	_, err := s.repo.sql.ExecContext(s.ctx, "DELETE FROM scheduler_outbox")
	s.Require().NoError(err, "清空 scheduler_outbox")

	err = s.repo.Create(s.ctx, account)
	s.Require().NoError(err, "Create")
	s.Require().NotZero(account.ID, "ID 应在 Create 后被回填")

	// 重新查出断言字段落库
	got, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().Equal("synced", got.Source, "source 字段落库正确")
	s.Require().NotNil(got.SyncSourceID, "sync_source_id 不应为 nil")
	s.Require().Equal(syncSourceID, *got.SyncSourceID, "sync_source_id 值落库正确")

	// 断言 scheduler_outbox 写入了 AccountChanged 行（blocking 回归）
	var outboxCount int
	s.Require().NoError(
		scanSingleRow(s.ctx, s.repo.sql,
			"SELECT COUNT(*) FROM scheduler_outbox WHERE event_type = $1",
			[]any{service.SchedulerOutboxEventAccountChanged},
			&outboxCount,
		),
		"查询 scheduler_outbox 行数",
	)
	s.Require().Equal(1, outboxCount, "Create 必须向 scheduler_outbox 写入一行 AccountChanged")
}

// TestCreate_DefaultSource 不设置 source 时，ent 默认值 "manual" 应生效。
func (s *AccountRepoSyncSuite) TestCreate_DefaultSource() {
	account := &service.Account{
		Name:        "manual-account",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Credentials: map[string]any{},
		Extra:       map[string]any{},
		Concurrency: 3,
		Priority:    50,
		Schedulable: true,
		// 不设置 Source，应落 ent 默认值 "manual"
	}

	err := s.repo.Create(s.ctx, account)
	s.Require().NoError(err, "Create")

	got, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().Equal("manual", got.Source, "未设置 source 时应落默认值 manual")
	s.Require().Nil(got.SyncSourceID, "未设置 sync_source_id 时应为 nil")
}

// --- FindGroupByName ---

// TestFindGroupByName_Found 按名称查到存在且未删除的分组。
func (s *AccountRepoSyncSuite) TestFindGroupByName_Found() {
	group := mustCreateGroup(s.T(), s.client, &service.Group{Name: "sync-group-find"})

	got, err := s.repo.FindGroupByName(s.ctx, "sync-group-find")
	s.Require().NoError(err, "FindGroupByName")
	s.Require().NotNil(got, "应命中已存在的分组")
	s.Require().Equal(group.ID, got.ID)
	s.Require().Equal("sync-group-find", got.Name)
}

// TestFindGroupByName_NotFound 查询不存在的名称应返回 nil（无错误）。
func (s *AccountRepoSyncSuite) TestFindGroupByName_NotFound() {
	got, err := s.repo.FindGroupByName(s.ctx, "no-such-group-xyz")
	s.Require().NoError(err, "FindGroupByName NotFound 不应返回错误")
	s.Require().Nil(got, "不存在的分组应返回 nil")
}

// TestFindGroupByName_SoftDeletedNotHit 同名但已软删除的分组不应命中。
func (s *AccountRepoSyncSuite) TestFindGroupByName_SoftDeletedNotHit() {
	// 创建分组后立即软删除
	group := mustCreateGroup(s.T(), s.client, &service.Group{Name: "sync-group-deleted"})
	now := time.Now()
	err := s.client.Group.UpdateOneID(group.ID).SetDeletedAt(now).Exec(s.ctx)
	s.Require().NoError(err, "设置 deleted_at")

	got, err := s.repo.FindGroupByName(s.ctx, "sync-group-deleted")
	s.Require().NoError(err, "FindGroupByName 软删除不应返回错误")
	s.Require().Nil(got, "软删除的分组不应被命中")
}

// --- FindProxyByIdentity ---

// TestFindProxyByIdentity_Found 按连接身份元组查到代理。
func (s *AccountRepoSyncSuite) TestFindProxyByIdentity_Found() {
	proxy := mustCreateProxy(s.T(), s.client, &service.Proxy{
		Name:     "sync-proxy-find",
		Protocol: "http",
		Host:     "proxy.example.com",
		Port:     8080,
		Username: "user1",
	})

	got, err := s.repo.FindProxyByIdentity(s.ctx, "http", "proxy.example.com", 8080, "user1")
	s.Require().NoError(err, "FindProxyByIdentity")
	s.Require().NotNil(got, "应命中已存在的代理")
	s.Require().Equal(proxy.ID, got.ID)
}

// TestFindProxyByIdentity_NotFound 查询不存在的元组应返回 nil（无错误）。
func (s *AccountRepoSyncSuite) TestFindProxyByIdentity_NotFound() {
	got, err := s.repo.FindProxyByIdentity(s.ctx, "http", "no-such-host.example.com", 9999, "nobody")
	s.Require().NoError(err, "FindProxyByIdentity NotFound 不应返回错误")
	s.Require().Nil(got, "不存在的代理应返回 nil")
}

// TestFindProxyByIdentity_MultiHitReturnsLatest 多个同元组代理时应按 created_at DESC 返回最新。
func (s *AccountRepoSyncSuite) TestFindProxyByIdentity_MultiHitReturnsLatest() {
	t1 := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	t2 := time.Now().Add(-1 * time.Minute).Truncate(time.Second)

	// 先建"旧"代理（created_at 更小），再建"新"代理
	older := mustCreateProxyWithCreatedAt(s.T(), s.client, &service.Proxy{
		Name:     "sync-proxy-older",
		Protocol: "socks5",
		Host:     "multi.example.com",
		Port:     1080,
		Username: "user2",
	}, t1)

	newer := mustCreateProxyWithCreatedAt(s.T(), s.client, &service.Proxy{
		Name:     "sync-proxy-newer",
		Protocol: "socks5",
		Host:     "multi.example.com",
		Port:     1080,
		Username: "user2",
	}, t2)

	got, err := s.repo.FindProxyByIdentity(s.ctx, "socks5", "multi.example.com", 1080, "user2")
	s.Require().NoError(err, "FindProxyByIdentity 多命中")
	s.Require().NotNil(got, "应命中一个代理")
	s.Require().Equal(newer.ID, got.ID, "多命中时应返回 created_at 最新的代理（older=%d newer=%d）", older.ID, newer.ID)
}

// mustCreateProxyWithCreatedAt 创建 created_at 可控的代理（用于多命中排序测试）。
func mustCreateProxyWithCreatedAt(t *testing.T, client *dbent.Client, p *service.Proxy, createdAt time.Time) *service.Proxy {
	t.Helper()
	if p.Protocol == "" {
		p.Protocol = "http"
	}
	if p.Host == "" {
		p.Host = "127.0.0.1"
	}
	if p.Port == 0 {
		p.Port = 8080
	}
	if p.Status == "" {
		p.Status = service.StatusActive
	}
	ctx := context.Background()
	create := client.Proxy.Create().
		SetName(p.Name).
		SetProtocol(p.Protocol).
		SetHost(p.Host).
		SetPort(p.Port).
		SetStatus(p.Status).
		SetCreatedAt(createdAt)
	if p.Username != "" {
		create.SetUsername(p.Username)
	}
	if p.Password != "" {
		create.SetPassword(p.Password)
	}
	created, err := create.Save(ctx)
	if err != nil {
		t.Fatalf("mustCreateProxyWithCreatedAt: %v", err)
	}
	p.ID = created.ID
	p.CreatedAt = created.CreatedAt
	return p
}
