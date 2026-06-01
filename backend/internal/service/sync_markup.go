package service

import (
	"context"
	"math"
	"os"
	"strconv"
	"strings"
)

// syncDefaultMarkupEnv 环境变量名：SYNC_DEFAULT_MARKUP
// 当数据库中 sync.default_markup 未设置时，读取此环境变量作为兜底。
const syncDefaultMarkupEnv = "SYNC_DEFAULT_MARKUP"

// syncDefaultMarkupFallback 硬编码兜底值：1.5 倍
// 含义：新建分组时加价 50%，避免零倍率把分组变成免费。
const syncDefaultMarkupFallback = 1.5

// parseSyncDefaultMarkup 纯函数：从原始字符串解析加价系数。
// raw 优先（来自 DB）；raw 为空或非法时尝试 envVal（来自 SYNC_DEFAULT_MARKUP）；
// 二者均无效（空/解析失败/<=0）则回退到 syncDefaultMarkupFallback。
// 这样把 "取值" 和 "解析+兜底" 分离，单测不需要 DB。
func parseSyncDefaultMarkup(raw, envVal string) float64 {
	for _, s := range []string{raw, envVal} {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			continue
		}
		return v
	}
	return syncDefaultMarkupFallback
}

// GetSyncDefaultMarkup 读取同步默认加价系数。
// 优先级：DB 设置 sync.default_markup > 环境变量 SYNC_DEFAULT_MARKUP > 硬编码 1.5。
// 解析失败或值 <=0 时一律兜底 1.5，不会返回导致免费的零倍率。
func (s *SettingService) GetSyncDefaultMarkup(ctx context.Context) float64 {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeySyncDefaultMarkup)
	if err != nil {
		// 包含 ErrSettingNotFound（未配置）和其他 DB 错误，均走 env/硬编码兜底
		raw = ""
	}
	return parseSyncDefaultMarkup(raw, os.Getenv(syncDefaultMarkupEnv))
}
