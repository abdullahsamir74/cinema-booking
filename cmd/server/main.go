package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/abdullahsamir74/cinema-booking/internal/adapters/memory"
	redisadapter "github.com/abdullahsamir74/cinema-booking/internal/adapters/redis"
	"github.com/abdullahsamir74/cinema-booking/internal/booking"
	"github.com/abdullahsamir74/cinema-booking/internal/catalog"
	"github.com/abdullahsamir74/cinema-booking/internal/config"
	"github.com/abdullahsamir74/cinema-booking/internal/httpapi"
)

// version is overridden at build time with:
//
//	go build -ldflags "-X main.version=1.0.0"
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	logger.Info("starting cinema booking service",
		"version", version,
		"env", cfg.Env,
		"store", cfg.Store,
		"hold_ttl", cfg.HoldTTL,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repository, closeRepository, err := newStores(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeRepository()

	service := booking.NewService(repository.bookings, repository.movies, cfg.HoldTTL)
	api := httpapi.NewServer(service, logger, version, cfg.CORSOrigins, cfg.AdminToken)

	if cfg.AdminToken == "" {
		logger.Warn("admin API disabled; set ADMIN_TOKEN to enable movie management")
	} else {
		logger.Info("admin API enabled", "route", "POST /api/v1/admin/movies")
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.Addr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("server stopped cleanly")
	return nil
}

func newStores(ctx context.Context, cfg config.Config, logger *slog.Logger) (stores, func(), error) {
	switch cfg.Store {
	case config.StoreRedis:
		client, err := redisadapter.NewClient(ctx, redisadapter.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		if err != nil {
			return stores{}, nil, fmt.Errorf("connect to redis: %w", err)
		}
		logger.Info("connected to redis", "addr", cfg.RedisAddr, "db", cfg.RedisDB)
		return stores{
			bookings: redisadapter.NewStore(client),
			movies:   redisadapter.NewMovieStore(client),
		}, func() { _ = client.Close() }, nil
	default:
		logger.Warn("using in-memory store; reservations and added movies are lost when the process stops")
		return stores{
			bookings: memory.New(),
			movies:   memory.NewCatalog(),
		}, func() {}, nil
	}
}

// stores groups the two repository ports used by the service.
type stores struct {
	bookings booking.Repository
	movies   catalog.Repository
}
