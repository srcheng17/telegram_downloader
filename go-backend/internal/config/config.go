package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr             string
	DatabaseURL      string
	UpstreamBaseURL  string
	InternalToken    string
	DownloadTimeout  int
	DownloadRetries  int
	ImageConcurrency int
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	ShutdownTimeout  time.Duration
}

func LoadFromEnv() (Config, error) {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "5000"
	}
	addr := port
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = strings.TrimSpace(os.Getenv("TASKS_DB_PATH"))
	}
	if databaseURL == "" {
		databaseURL = "postgresql://telegraph:telegraph@localhost:5432/telegraph?sslmode=disable"
	}
	if !isPostgresURL(databaseURL) {
		return Config{}, fmt.Errorf("go-backend only supports PostgreSQL in phase 1, got TASKS_DB_PATH=%q", databaseURL)
	}

	return Config{
		Addr:             addr,
		DatabaseURL:      databaseURL,
		UpstreamBaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("PYTHON_WEB_BASE_URL")), "/"),
		InternalToken:    strings.TrimSpace(os.Getenv("INTERNAL_ENQUEUE_TOKEN")),
		DownloadTimeout:  parseIntEnv("GO_DOWNLOAD_TIMEOUT", 30),
		DownloadRetries:  parseIntEnv("GO_DOWNLOAD_RETRIES", 10),
		ImageConcurrency: parseIntEnv("GO_IMAGE_CONCURRENCY", 2),
		ReadTimeout:      15 * time.Second,
		WriteTimeout:     30 * time.Second,
		IdleTimeout:      60 * time.Second,
		ShutdownTimeout:  10 * time.Second,
	}, nil
}

func isPostgresURL(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(normalized, "postgresql://") || strings.HasPrefix(normalized, "postgres://")
}

func parseIntEnv(name string, defaultValue int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}
