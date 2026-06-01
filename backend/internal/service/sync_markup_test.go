//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// --- parseSyncDefaultMarkup 纯函数测试（不依赖 DB / repo）---

// TestParseSyncDefaultMarkup_EmptyRaw_EmptyEnv：无 DB 值、无 env → 兜底 1.5
func TestParseSyncDefaultMarkup_EmptyRaw_EmptyEnv(t *testing.T) {
	got := parseSyncDefaultMarkup("", "")
	require.Equal(t, syncDefaultMarkupFallback, got)
}

// TestParseSyncDefaultMarkup_ValidRaw：DB 写入合法值 → 直接返回
func TestParseSyncDefaultMarkup_ValidRaw(t *testing.T) {
	got := parseSyncDefaultMarkup("2.0", "")
	require.InDelta(t, 2.0, got, 1e-9)
}

// TestParseSyncDefaultMarkup_EnvFallback：DB 无值、env 有值 → 返回 env 值
func TestParseSyncDefaultMarkup_EnvFallback(t *testing.T) {
	got := parseSyncDefaultMarkup("", "1.8")
	require.InDelta(t, 1.8, got, 1e-9)
}

// TestParseSyncDefaultMarkup_InvalidRaw：DB 值解析失败 → 兜底 1.5
func TestParseSyncDefaultMarkup_InvalidRaw(t *testing.T) {
	got := parseSyncDefaultMarkup("not-a-number", "")
	require.Equal(t, syncDefaultMarkupFallback, got)
}

// TestParseSyncDefaultMarkup_ZeroRaw：DB 值为 0（无效倍率）→ 兜底 1.5
func TestParseSyncDefaultMarkup_ZeroRaw(t *testing.T) {
	got := parseSyncDefaultMarkup("0", "")
	require.Equal(t, syncDefaultMarkupFallback, got)
}

// TestParseSyncDefaultMarkup_NegativeRaw：DB 值为负 → 兜底 1.5
func TestParseSyncDefaultMarkup_NegativeRaw(t *testing.T) {
	got := parseSyncDefaultMarkup("-1.5", "")
	require.Equal(t, syncDefaultMarkupFallback, got)
}

// TestParseSyncDefaultMarkup_InvalidEnv：env 也解析失败 → 兜底 1.5
func TestParseSyncDefaultMarkup_InvalidEnv(t *testing.T) {
	got := parseSyncDefaultMarkup("", "bad-env")
	require.Equal(t, syncDefaultMarkupFallback, got)
}

// TestParseSyncDefaultMarkup_ZeroEnv：env 为 0（无效倍率）→ 兜底 1.5
func TestParseSyncDefaultMarkup_ZeroEnv(t *testing.T) {
	got := parseSyncDefaultMarkup("", "0")
	require.Equal(t, syncDefaultMarkupFallback, got)
}

// TestParseSyncDefaultMarkup_DBOverridesEnv：DB 有值时 env 被忽略
func TestParseSyncDefaultMarkup_DBOverridesEnv(t *testing.T) {
	got := parseSyncDefaultMarkup("3.0", "1.8")
	require.InDelta(t, 3.0, got, 1e-9)
}

// --- GetSyncDefaultMarkup 集成桩测试 ---

// markupRepoStub 只实现 GetValue，其余方法一旦被调用立刻 panic。
type markupRepoStub struct {
	values map[string]string
	err    error
}

func (r *markupRepoStub) Get(_ context.Context, _ string) (*Setting, error) {
	panic("unexpected Get")
}
func (r *markupRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if v, ok := r.values[key]; ok {
		return v, nil
	}
	return "", ErrSettingNotFound
}
func (r *markupRepoStub) Set(_ context.Context, _, _ string) error { panic("unexpected Set") }
func (r *markupRepoStub) GetMultiple(_ context.Context, _ []string) (map[string]string, error) {
	panic("unexpected GetMultiple")
}
func (r *markupRepoStub) SetMultiple(_ context.Context, _ map[string]string) error {
	panic("unexpected SetMultiple")
}
func (r *markupRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	panic("unexpected GetAll")
}
func (r *markupRepoStub) Delete(_ context.Context, _ string) error { panic("unexpected Delete") }

// TestGetSyncDefaultMarkup_NoSetting_NoEnv → 返回 1.5
func TestGetSyncDefaultMarkup_NoSetting_NoEnv(t *testing.T) {
	repo := &markupRepoStub{} // 返回 ErrSettingNotFound
	svc := NewSettingService(repo, &config.Config{})
	t.Setenv(syncDefaultMarkupEnv, "")

	got := svc.GetSyncDefaultMarkup(context.Background())
	require.Equal(t, syncDefaultMarkupFallback, got)
}

// TestGetSyncDefaultMarkup_SettingSet → 返回 DB 值
func TestGetSyncDefaultMarkup_SettingSet(t *testing.T) {
	repo := &markupRepoStub{values: map[string]string{
		SettingKeySyncDefaultMarkup: "2.0",
	}}
	svc := NewSettingService(repo, &config.Config{})

	got := svc.GetSyncDefaultMarkup(context.Background())
	require.InDelta(t, 2.0, got, 1e-9)
}

// TestGetSyncDefaultMarkup_EnvFallback → DB 无值、env 有值
func TestGetSyncDefaultMarkup_EnvFallback(t *testing.T) {
	repo := &markupRepoStub{} // 返回 ErrSettingNotFound
	svc := NewSettingService(repo, &config.Config{})
	t.Setenv(syncDefaultMarkupEnv, "1.8")

	got := svc.GetSyncDefaultMarkup(context.Background())
	require.InDelta(t, 1.8, got, 1e-9)
}

// TestGetSyncDefaultMarkup_DBError → DB 报错 → 兜底 1.5
func TestGetSyncDefaultMarkup_DBError(t *testing.T) {
	repo := &markupRepoStub{err: errors.New("db down")}
	svc := NewSettingService(repo, &config.Config{})
	t.Setenv(syncDefaultMarkupEnv, "")

	got := svc.GetSyncDefaultMarkup(context.Background())
	require.Equal(t, syncDefaultMarkupFallback, got)
}
