package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// ssoPurgeAPIKeyRepoStub 是内存版 APIKeyRepository：只实现回收路径用到的方法。
// 嵌入接口可让未实现的方法在被调用时 panic（提示测试触达了非预期路径）。
type ssoPurgeAPIKeyRepoStub struct {
	service.APIKeyRepository
	keys map[int64]*service.APIKey
}

func (s *ssoPurgeAPIKeyRepoStub) ListByUserID(
	ctx context.Context,
	userID int64,
	params pagination.PaginationParams,
	filters service.APIKeyListFilters,
) ([]service.APIKey, *pagination.PaginationResult, error) {
	out := make([]service.APIKey, 0)
	for _, k := range s.keys {
		if k.UserID != userID {
			continue
		}
		// 模拟真实 repo 的 Search：按 Name 子串模糊匹配（够覆盖本测试意图）。
		if filters.Search != "" && !strings.Contains(k.Name, filters.Search) {
			continue
		}
		out = append(out, *k)
	}
	return out, &pagination.PaginationResult{}, nil
}

func (s *ssoPurgeAPIKeyRepoStub) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	k, ok := s.keys[id]
	if !ok {
		return "", 0, service.ErrAPIKeyNotFound
	}
	return k.Key, k.UserID, nil
}

func (s *ssoPurgeAPIKeyRepoStub) Delete(ctx context.Context, id int64) error {
	delete(s.keys, id)
	return nil
}

// 回收必须只删「当前用户的、精确名为 studio-sso」的 key：
// 用户自建的其它 key、以及他人的 studio-sso key 都必须原样保留。
func TestRevokePreviousTempKeys_PurgesOnlyOwnSSOKeys(t *testing.T) {
	repo := &ssoPurgeAPIKeyRepoStub{keys: map[int64]*service.APIKey{
		1: {ID: 1, UserID: 42, Key: "sk-a", Name: ssoTempKeyName},
		2: {ID: 2, UserID: 42, Key: "sk-b", Name: ssoTempKeyName},
		3: {ID: 3, UserID: 42, Key: "sk-c", Name: "我的自用key"},  // 同用户但非临时 key，保留
		4: {ID: 4, UserID: 99, Key: "sk-d", Name: ssoTempKeyName}, // 他人的临时 key，保留
	}}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)
	h := &SSOHandler{apiKeyService: svc}

	h.revokePreviousTempKeys(context.Background(), 42)

	require.NotContains(t, repo.keys, int64(1), "用户自己的旧临时 key 应被回收")
	require.NotContains(t, repo.keys, int64(2), "用户自己的旧临时 key 应被回收")
	require.Contains(t, repo.keys, int64(3), "用户自建的非临时 key 必须保留")
	require.Contains(t, repo.keys, int64(4), "他人的临时 key 必须保留")
}
