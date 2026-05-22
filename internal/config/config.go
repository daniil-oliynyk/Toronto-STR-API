package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL        string
	Port               string
	CORSAllowedOrigins []string
}

func Load() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("SUPABASE_URL"))
	if databaseURL == "" {
		return Config{}, fmt.Errorf("SUPABASE_URL is required")
	}
	if _, err := url.ParseRequestURI(databaseURL); err != nil {
		return Config{}, fmt.Errorf("SUPABASE_URL is invalid: %w", err)
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	if err := validatePort(port); err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:        databaseURL,
		Port:               port,
		CORSAllowedOrigins: parseList(os.Getenv("CORS_ALLOWED_ORIGINS")),
	}, nil
}

func validatePort(port string) error {
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 {
		return fmt.Errorf("PORT must be a number between 1 and 65535")
	}

	return nil
}

func parseList(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		values = append(values, trimmed)
	}

	return values
}
