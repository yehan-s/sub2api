//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// TestImport_DisabledReturnsError 总闸关闭时 Import 直接报错，不触达任何依赖。
func TestImport_DisabledReturnsError(t *testing.T) {
	s := NewAccountSyncService(nil, nil, nil, nil, config.ResellerSyncConfig{Enabled: false})
	_, err := s.Import(context.Background(), []int64{1})
	require.Error(t, err)
}

// TestPreview_DisabledReturnsError 总闸关闭时 Preview 直接报错。
func TestPreview_DisabledReturnsError(t *testing.T) {
	s := NewAccountSyncService(nil, nil, nil, nil, config.ResellerSyncConfig{Enabled: false})
	_, err := s.Preview(context.Background())
	require.Error(t, err)
}

// TestDiffCandidates_ExcludesAlreadySynced 已同步(按 sync_source_id)的应被排除。
func TestDiffCandidates_ExcludesAlreadySynced(t *testing.T) {
	prod := []SyncSourceAccount{{ID: 1}, {ID: 2}, {ID: 3}}
	existing := map[int64]bool{1: true, 3: true}

	got := diffCandidates(prod, existing)

	require.Len(t, got, 1)
	require.Equal(t, int64(2), got[0].ID)
}

// TestDiffCandidates_AllNew 本地无任何已同步时，全部生产账号都是候选。
func TestDiffCandidates_AllNew(t *testing.T) {
	prod := []SyncSourceAccount{{ID: 5}, {ID: 6}}

	got := diffCandidates(prod, map[int64]bool{})

	require.Len(t, got, 2)
}

// TestDiffCandidates_AllExisting 全部已同步时返回空（非 nil）。
func TestDiffCandidates_AllExisting(t *testing.T) {
	prod := []SyncSourceAccount{{ID: 1}, {ID: 2}}
	existing := map[int64]bool{1: true, 2: true}

	got := diffCandidates(prod, existing)

	require.NotNil(t, got)
	require.Len(t, got, 0)
}

// TestDiffCandidates_EmptyProd 生产为空时返回空。
func TestDiffCandidates_EmptyProd(t *testing.T) {
	got := diffCandidates(nil, map[int64]bool{1: true})
	require.Len(t, got, 0)
}
