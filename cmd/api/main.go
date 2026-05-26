package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/daniil-oliynyk/go-api/internal/config"
	"github.com/daniil-oliynyk/go-api/internal/httpapi"
	"github.com/daniil-oliynyk/go-api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	debugLogs := flag.Bool("debug", false, "enable debug logs")
	flag.Parse()

	logLevel := slog.LevelInfo
	if *debugLogs {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)
	logger.Debug("function entry", "function", "main")
	defer logger.Debug("function exit", "function", "main")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("api config invalid", "error", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("database pool initialization failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	listingsRepository := store.NewListingsRepository(pool)
	router := httpapi.NewRouter(httpapi.Dependencies{
		ReadinessChecker: pool,
		MetadataProvider: listingsRepository,
		ListingProvider:  listingsRepository,
		MapProvider:      listingsRepository,
		StatsProvider:    listingsRepository,
		CORSOrigins:      cfg.CORSAllowedOrigins,
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("api server starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("api server shutting down")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("api server shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("api server stopped")
}
