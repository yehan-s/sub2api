//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// TestOpenSourceClient_DisabledReturnsError 验证 Enabled=false 时直接返回错误，不尝试连库。
func TestOpenSourceClient_DisabledReturnsError(t *testing.T) {
	cfg := config.ResellerSyncConfig{
		Enabled:      false,
		SourceDBHost: "db.prod.internal",
		SourceDBPort: 5432,
		SourceDBName: "sub2api",
		SourceDBUser: "ro_user",
		SourceDBPass: "ro_pass",
	}
	client, err := OpenSourceClient(cfg)
	if err == nil {
		_ = client.Close()
		t.Fatal("期望 Enabled=false 时返回 error，但 err==nil")
	}
}

// TestOpenSourceClient_MissingParamsReturnsError 验证参数缺失时返回"参数不完整"错误，不尝试连库。
func TestOpenSourceClient_MissingParamsReturnsError(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.ResellerSyncConfig
	}{
		{
			name: "host 为空",
			cfg: config.ResellerSyncConfig{
				Enabled:      true,
				SourceDBHost: "",
				SourceDBUser: "ro_user",
				SourceDBPass: "ro_pass",
			},
		},
		{
			name: "user 为空",
			cfg: config.ResellerSyncConfig{
				Enabled:      true,
				SourceDBHost: "db.prod.internal",
				SourceDBUser: "",
				SourceDBPass: "ro_pass",
			},
		},
		{
			name: "pass 为空",
			cfg: config.ResellerSyncConfig{
				Enabled:      true,
				SourceDBHost: "db.prod.internal",
				SourceDBUser: "ro_user",
				SourceDBPass: "",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := OpenSourceClient(tc.cfg)
			if err == nil {
				_ = client.Close()
				t.Fatalf("[%s] 期望参数不完整时返回 error，但 err==nil", tc.name)
			}
		})
	}
}
