package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL        string
	Port               string
	CORSAllowedOrigins []string
	InternalAPIKey     string
}

func Load() (Config, error) {
	slog.Debug("function entry", "function", "config.Load")
	defer slog.Debug("function exit", "function", "config.Load")

	databaseURL := strings.TrimSpace(os.Getenv("SUPABASE_URL"))
	if databaseURL == "" {
		slog.Error("config load failed", "error", "SUPABASE_URL is required")
		return Config{}, fmt.Errorf("SUPABASE_URL is required")
	}
	if _, err := url.ParseRequestURI(databaseURL); err != nil {
		slog.Error("config load failed", "error", err)
		return Config{}, fmt.Errorf("SUPABASE_URL is invalid: %w", err)
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	if err := validatePort(port); err != nil {
		slog.Error("config load failed", "error", err)
		return Config{}, err
	}

	corsOrigins := parseList(os.Getenv("CORS_ALLOWED_ORIGINS"))
	internalAPIKey := strings.TrimSpace(os.Getenv("INTERNAL_API_KEY"))
	slog.Info(
		"config loaded",
		"port", port,
		"cors_origin_count", len(corsOrigins),
		"internal_api_key_configured", internalAPIKey != "",
	)

	return Config{
		DatabaseURL:        databaseURL,
		Port:               port,
		CORSAllowedOrigins: corsOrigins,
		InternalAPIKey:     internalAPIKey,
	}, nil
}

func validatePort(port string) error {
	slog.Debug("function entry", "function", "config.validatePort", "port", port)
	defer slog.Debug("function exit", "function", "config.validatePort")

	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 {
		slog.Warn("port validation failed", "port", port)
		return fmt.Errorf("PORT must be a number between 1 and 65535")
	}

	return nil
}

func parseList(value string) []string {
	slog.Debug("function entry", "function", "config.parseList", "empty", strings.TrimSpace(value) == "")
	defer slog.Debug("function exit", "function", "config.parseList")

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		values = append(values, trimmed)
	}

	slog.Debug("list parsed", "count", len(values))
	return values
}
