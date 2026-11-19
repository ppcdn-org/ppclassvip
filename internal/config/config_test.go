package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PPCLASS_ENV", "dev")
	t.Setenv("PPCLASS_JWT_SECRET", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.App.Name != "PPClassVip" {
		t.Fatalf("App.Name = %q", cfg.App.Name)
	}
	if cfg.Auth.JWTSecret != DefaultDevJWTSecret {
		t.Fatalf("dev JWT secret was not applied")
	}
}

func TestLoadYAMLAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
http:
  readTimeout: 2s
  writeTimeout: 3s
  shutdownTimeout: 4s
mysql:
  dsn: "root@tcp(127.0.0.1:3306)/test?parseTime=true"
  maxOpenConns: 4
  maxIdleConns: 2
  connMaxLifetime: 5m
  autoMigrate: false
auth:
  issuer: test-issuer
  accessTTL: 6h
`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PPCLASS_HTTP_ADDR", ":9090")
	t.Setenv("PPCLASS_ENV", "dev")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Addr != ":9090" {
		t.Fatalf("HTTP.Addr = %q", cfg.HTTP.Addr)
	}
	if cfg.HTTP.ReadTimeout.Std() != 2*time.Second {
		t.Fatalf("ReadTimeout = %v", cfg.HTTP.ReadTimeout.Std())
	}
	if cfg.MySQL.MaxOpenConns != 4 || cfg.MySQL.MaxIdleConns != 2 {
		t.Fatalf("mysql pool config not loaded")
	}
	if cfg.MySQL.AutoMigrate {
		t.Fatalf("AutoMigrate should be false")
	}
	if cfg.Auth.AccessTTL.Std() != 6*time.Hour {
		t.Fatalf("AccessTTL = %v", cfg.Auth.AccessTTL.Std())
	}
}

func TestProdRequiresStrongJWTSecret(t *testing.T) {
	t.Setenv("PPCLASS_ENV", "prod")
	t.Setenv("PPCLASS_JWT_SECRET", "too-short")

	if _, err := Load(""); err == nil {
		t.Fatal("Load() expected error for short prod JWT secret")
	}
}

func TestProdAcceptsStrongJWTSecret(t *testing.T) {
	t.Setenv("PPCLASS_ENV", "prod")
	t.Setenv("PPCLASS_JWT_SECRET", "0123456789abcdef0123456789abcdef")

	if _, err := Load(""); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}
