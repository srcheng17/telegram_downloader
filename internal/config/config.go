package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeoutSeconds   = 30
	defaultRetries          = 10
	defaultImageConcurrency = 2
	defaultDownloadAction   = "browser"

	minTimeoutSeconds   = 1
	maxTimeoutSeconds   = 300
	minRetries          = 0
	maxRetries          = 20
	minImageConcurrency = 1
	maxImageConcurrency = 20
)

type SettingsSnapshot struct {
	Timeout            int    `json:"timeout"`
	Retries            int    `json:"retries"`
	ImageConcurrency   int    `json:"image_concurrency"`
	DownloadActionMode string `json:"download_action_mode"`
}

type Config struct {
	Addr             string
	DatabaseURL      string
	ConsumerName     string
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

	production := isProductionEnv()

	rawDatabaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if production && rawDatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required in production")
	}

	databaseURL := rawDatabaseURL
	if databaseURL == "" {
		databaseURL = strings.TrimSpace(os.Getenv("TASKS_DB_PATH"))
	}
	if databaseURL == "" {
		databaseURL = "postgresql://telegraph:telegraph@localhost:5432/telegraph?sslmode=disable"
	}
	if !isPostgresURL(databaseURL) {
		return Config{}, fmt.Errorf("go-backend only supports PostgreSQL in phase 1, got TASKS_DB_PATH=%q", databaseURL)
	}

	consumerName := strings.TrimSpace(os.Getenv("CONSUMER_NAME"))
	if consumerName == "" {
		consumerName = strings.TrimSpace(os.Getenv("HOSTNAME"))
	}

	defaultSettings := DefaultSettingsSnapshot()
	settings := NormalizeSettingsSnapshot(SettingsSnapshot{
		Timeout:          parseIntEnv("GO_DOWNLOAD_TIMEOUT", defaultSettings.Timeout),
		Retries:          parseIntEnv("GO_DOWNLOAD_RETRIES", defaultSettings.Retries),
		ImageConcurrency: parseIntEnv("GO_IMAGE_CONCURRENCY", defaultSettings.ImageConcurrency),
	})

	internalToken := strings.TrimSpace(os.Getenv("INTERNAL_ENQUEUE_TOKEN"))
	if production && internalToken == "" {
		return Config{}, errors.New("INTERNAL_ENQUEUE_TOKEN is required in production")
	}

	return Config{
		Addr:             addr,
		DatabaseURL:      databaseURL,
		ConsumerName:     consumerName,
		UpstreamBaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("PYTHON_WEB_BASE_URL")), "/"),
		InternalToken:    internalToken,
		DownloadTimeout:  settings.Timeout,
		DownloadRetries:  settings.Retries,
		ImageConcurrency: settings.ImageConcurrency,
		ReadTimeout:      15 * time.Second,
		WriteTimeout:     30 * time.Second,
		IdleTimeout:      60 * time.Second,
		ShutdownTimeout:  10 * time.Second,
	}, nil
}

func isProductionEnv() bool {
	candidates := []string{
		os.Getenv("APP_ENV"),
		os.Getenv("GO_ENV"),
		os.Getenv("ENV"),
	}
	for _, candidate := range candidates {
		switch strings.ToLower(strings.TrimSpace(candidate)) {
		case "production", "prod":
			return true
		}
	}
	return false
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

func DefaultSettingsSnapshot() SettingsSnapshot {
	return SettingsSnapshot{
		Timeout:            defaultTimeoutSeconds,
		Retries:            defaultRetries,
		ImageConcurrency:   defaultImageConcurrency,
		DownloadActionMode: defaultDownloadAction,
	}
}

func NormalizeSettingsSnapshot(input SettingsSnapshot) SettingsSnapshot {
	return SettingsSnapshot{
		Timeout:            clampInt(input.Timeout, minTimeoutSeconds, maxTimeoutSeconds),
		Retries:            clampInt(input.Retries, minRetries, maxRetries),
		ImageConcurrency:   clampInt(input.ImageConcurrency, minImageConcurrency, maxImageConcurrency),
		DownloadActionMode: normalizeDownloadActionMode(input.DownloadActionMode),
	}
}

func normalizeDownloadActionMode(raw string) string {
	switch strings.TrimSpace(raw) {
	case "komga_copy":
		return "komga_copy"
	default:
		return defaultDownloadAction
	}
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
