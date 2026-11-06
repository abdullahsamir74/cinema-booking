package config

import (
	"log/slog"
	"testing"
	"time"
)

func reset(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_ENV", "APP_ADDR", "BOOKING_STORE", "REDIS_ADDR", "REDIS_PASSWORD",
		"REDIS_DB", "SHUTDOWN_TIMEOUT", "HOLD_TTL", "LOG_LEVEL", "ADMIN_TOKEN",
		"CORS_ALLOWED_ORIGINS", "SERVER_READ_HEADER_TIMEOUT", "SERVER_READ_TIMEOUT",
		"SERVER_WRITE_TIMEOUT", "SERVER_IDLE_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	reset(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Store != StoreMemory {
		t.Fatalf("expected memory store, got %q", cfg.Store)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("expected :8080, got %q", cfg.Addr)
	}
	if cfg.HoldTTL != 2*time.Minute {
		t.Fatalf("expected 2m hold ttl, got %s", cfg.HoldTTL)
	}
	if len(cfg.CORSOrigins) != 1 || cfg.CORSOrigins[0] != "*" {
		t.Fatalf("unexpected cors origins: %v", cfg.CORSOrigins)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected log level: %v", cfg.LogLevel)
	}
}

func TestLoadCustomValues(t *testing.T) {
	reset(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_ADDR", ":9000")
	t.Setenv("BOOKING_STORE", "redis")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("REDIS_DB", "3")
	t.Setenv("HOLD_TTL", "90s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("ADMIN_TOKEN", "custom-admin-token")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://a.example, https://b.example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.IsProduction() {
		t.Fatal("expected production mode")
	}
	if cfg.Store != StoreRedis || cfg.RedisAddr != "redis:6379" || cfg.RedisDB != 3 {
		t.Fatalf("unexpected redis config: %+v", cfg)
	}
	if cfg.HoldTTL != 90*time.Second {
		t.Fatalf("unexpected hold ttl: %s", cfg.HoldTTL)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("unexpected log level: %v", cfg.LogLevel)
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Fatalf("unexpected origins: %v", cfg.CORSOrigins)
	}
	if cfg.AdminToken != "custom-admin-token" {
		t.Fatalf("unexpected admin token: %q", cfg.AdminToken)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"unknown store", "BOOKING_STORE", "postgres"},
		{"invalid duration", "HOLD_TTL", "two-minutes"},
		{"zero hold ttl", "HOLD_TTL", "0s"},
		{"invalid db", "REDIS_DB", "abc"},
		{"empty origins", "CORS_ALLOWED_ORIGINS", ","},
		{"short admin token", "ADMIN_TOKEN", "short"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reset(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
