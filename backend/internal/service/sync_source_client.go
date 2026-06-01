package service

import (
	"errors"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq" // PostgreSQL 驱动副作用导入，与主连接保持一致
)

// OpenSourceClient 按 ResellerSync 配置打开一个只读用途的 ent client 连到生产库。
//
// 只读语义由两层保证：
//  1. 生产侧 sync_ro 角色仅持有 SELECT 权限；
//  2. 调用方只做 Query，不调用任何写操作。
//
// 调用方负责 defer client.Close()，用完即关（短连，不常驻）。
func OpenSourceClient(cfg config.ResellerSyncConfig) (*dbent.Client, error) {
	if !cfg.Enabled {
		return nil, errors.New("分销同步未启用")
	}
	if cfg.SourceDBHost == "" || cfg.SourceDBUser == "" || cfg.SourceDBPass == "" {
		return nil, errors.New("生产库只读连接参数不完整")
	}

	// DSN 格式与主连接（repository/ent.go）保持一致：key=value libpq 形式。
	// sslmode=disable 由调用侧网络/VPN 保证安全，与内网部署场景对齐。
	port := cfg.SourceDBPort
	if port == 0 {
		port = 5432
	}
	dbname := cfg.SourceDBName
	if dbname == "" {
		dbname = "sub2api"
	}
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.SourceDBHost, port, cfg.SourceDBUser, cfg.SourceDBPass, dbname,
	)

	// 使用与主连接相同的 entsql.Open + dialect.Postgres + lib/pq 组合。
	drv, err := entsql.Open(dialect.Postgres, dsn)
	if err != nil {
		return nil, fmt.Errorf("连不上生产库: %w", err)
	}

	return dbent.NewClient(dbent.Driver(drv)), nil
}
