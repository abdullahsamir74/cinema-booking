// Package config loads application settings from environment variables.
// It has no knowledge of the concrete storage or HTTP frameworks used.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// StoreKind selects the booking repository implementation.
type StoreKind string

const (
	StoreMemory StoreKind = "memory"
	StoreRedis  StoreKind = "redis"
)

// Config is the complete runtime configuration.
type Config struct {
	Env             string
	Addr            string
	Store           StoreKind
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	AdminToken      string
	HoldTTL         time.Duration
	LogLevel        slog.Level
	CORSOrigins     []string
	ShutdownTimeout time.Duration

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// Load parses the environment and validates the result.
func Load() (Config, error) {
	var err error

	cfg := Config{
		Env:           getenv("APP_ENV", "development"),
		Addr:          getenv("APP_ADDR", ":8080"),
		Store:         StoreKind(strings.ToLower(getenv("BOOKING_STORE", string(StoreMemory)))),
		RedisAddr:     getenv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		AdminToken:    getenv("ADMIN_TOKEN", ""),
		CORSOrigins:   splitCSV(getenv("CORS_ALLOWED_ORIGINS", "*")),
		LogLevel:      parseLevel(getenv("LOG_LEVEL", "info")),
	}

	if cfg.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ReadHeaderTimeout, err = durationEnv("SERVER_READ_HEADER_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ReadTimeout, err = durationEnv("SERVER_READ_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = durationEnv("SERVER_WRITE_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = durationEnv("SERVER_IDLE_TIMEOUT", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.HoldTTL, err = durationEnv("HOLD_TTL", 2*time.Minute); err != nil {
		return Config{}, err
	}

	db, err := strconv.Atoi(getenv("REDIS_DB", "0"))
	if err != nil {
		return Config{}, fmt.Errorf("parse REDIS_DB: %w", err)
	}
	cfg.RedisDB = db

	switch cfg.Store {
	case StoreMemory, StoreRedis:
	default:
		return Config{}, fmt.Errorf("BOOKING_STORE must be %q or %q, got %q", StoreMemory, StoreRedis, cfg.Store)
	}
	if cfg.HoldTTL <= 0 {
		return Config{}, fmt.Errorf("HOLD_TTL must be positive")
	}
	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("APP_ADDR must not be empty")
	}
	if len(cfg.CORSOrigins) == 0 {
		return Config{}, fmt.Errorf("CORS_ALLOWED_ORIGINS must contain at least one origin")
	}
	if cfg.AdminToken != "" && len(cfg.AdminToken) < 8 {
		return Config{}, fmt.Errorf("ADMIN_TOKEN must be at least 8 characters when set")
	}
	return cfg, nil
}

// IsProduction reports whether the app runs in production mode.
func (c Config) IsProduction() bool {
	return strings.EqualFold(c.Env, "production")
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := getenv(key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return value, nil
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
