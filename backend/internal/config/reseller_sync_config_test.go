//go:build unit

package config

import (
	"testing"

	"github.com/spf13/viper"
)

// TestResellerSyncConfigDefaultsDisabled 验证默认情况下分销同步功能关闭。
func TestResellerSyncConfigDefaultsDisabled(t *testing.T) {
	resetViperWithJWTSecret(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ResellerSync.Enabled {
		t.Fatalf("ResellerSync.Enabled 默认应为 false，实际为 true")
	}
	if cfg.ResellerSync.SourceDBHost != "" {
		t.Fatalf("ResellerSync.SourceDBHost 默认应为空，实际为 %q", cfg.ResellerSync.SourceDBHost)
	}
	if cfg.ResellerSync.SourceDBPort != 5432 {
		t.Fatalf("ResellerSync.SourceDBPort 默认应为 5432，实际为 %d", cfg.ResellerSync.SourceDBPort)
	}
	if cfg.ResellerSync.SourceDBName != "sub2api" {
		t.Fatalf("ResellerSync.SourceDBName 默认应为 sub2api，实际为 %q", cfg.ResellerSync.SourceDBName)
	}
	if cfg.ResellerSync.SourceDBUser != "" {
		t.Fatalf("ResellerSync.SourceDBUser 默认应为空，实际为 %q", cfg.ResellerSync.SourceDBUser)
	}
	if cfg.ResellerSync.SourceDBPass != "" {
		t.Fatalf("ResellerSync.SourceDBPass 默认应为空，实际为 %q", cfg.ResellerSync.SourceDBPass)
	}
}

// TestResellerSyncConfigFromEnv 验证环境变量经 AutomaticEnv+SetEnvKeyReplacer 映射后能正确读取。
func TestResellerSyncConfigFromEnv(t *testing.T) {
	viper.Reset()
	t.Setenv("JWT_SECRET", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	t.Setenv("RESELLER_SYNC_ENABLED", "true")
	t.Setenv("RESELLER_SYNC_SOURCE_DB_HOST", "db.prod.internal")
	t.Setenv("RESELLER_SYNC_SOURCE_DB_PORT", "5433")
	t.Setenv("RESELLER_SYNC_SOURCE_DB_NAME", "prod_sub2api")
	t.Setenv("RESELLER_SYNC_SOURCE_DB_USER", "ro_user")
	t.Setenv("RESELLER_SYNC_SOURCE_DB_PASSWORD", "ro_pass")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.ResellerSync.Enabled {
		t.Fatalf("ResellerSync.Enabled 应为 true")
	}
	if cfg.ResellerSync.SourceDBHost != "db.prod.internal" {
		t.Fatalf("ResellerSync.SourceDBHost = %q，want %q", cfg.ResellerSync.SourceDBHost, "db.prod.internal")
	}
	if cfg.ResellerSync.SourceDBPort != 5433 {
		t.Fatalf("ResellerSync.SourceDBPort = %d，want 5433", cfg.ResellerSync.SourceDBPort)
	}
	if cfg.ResellerSync.SourceDBName != "prod_sub2api" {
		t.Fatalf("ResellerSync.SourceDBName = %q，want %q", cfg.ResellerSync.SourceDBName, "prod_sub2api")
	}
	if cfg.ResellerSync.SourceDBUser != "ro_user" {
		t.Fatalf("ResellerSync.SourceDBUser = %q，want %q", cfg.ResellerSync.SourceDBUser, "ro_user")
	}
	if cfg.ResellerSync.SourceDBPass != "ro_pass" {
		t.Fatalf("ResellerSync.SourceDBPass = %q，want %q", cfg.ResellerSync.SourceDBPass, "ro_pass")
	}
}
