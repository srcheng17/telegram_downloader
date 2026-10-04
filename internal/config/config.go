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
	MaxImagesPerTask       = 300
	MaxBytesPerImage int64 = 25 << 20
	MaxBytesPerTask  int64 = 500 << 20
	MaxUploadBytes   int64 = 64 << 20

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

var ErrInvalidSettings = errors.New("invalid download settings")
var ErrSettingsConflict = errors.New("download settings version conflict")

type SettingsSnapshot struct {
	Timeout            int    `json:"timeout"`
	Retries            int    `json:"retries"`
	ImageConcurrency   int    `json:"image_concurrency"`
	DownloadActionMode string `json:"download_action_mode"`
}

// VersionedSettingsSnapshot is the administrator-facing CAS contract used by
// the CLI. Legacy /v2/settings reads remain compatible with SettingsSnapshot.
type VersionedSettingsSnapshot struct {
	SettingsSnapshot
	ConfigVersion int64 `json:"config_version"`
}

type Config struct {
	SourceRetentionRoot    string
	TelegramMaxSourceBytes int64
	TelegramEnabled        bool
	TelegramPrivateRoot    string
	TelegramHelperPath     string
	TelegramTDLPath        string
	KomgaLibraryMappings   []KomgaLibraryMapping
	KomgaReadOnlyLibraries []string
	KomgaEditBackupRoot    string
	PublicOrigin           string
	AllowInsecureLoopback  bool
	Addr                   string
	DatabaseURL            string
	ConsumerName           string
	UpstreamBaseURL        string
	InternalToken          string
	DownloadTimeout        int
	DownloadRetries        int
	ImageConcurrency       int
	ReadTimeout            time.Duration
	WriteTimeout           time.Duration
	IdleTimeout            time.Duration
	ShutdownTimeout        time.Duration
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

	telegramMaxSourceBytes := int64(500 << 20)
	if value := strings.TrimSpace(os.Getenv("TELEGRAM_MAX_SOURCE_BYTES")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 1 || parsed > 500<<20 {
			return Config{}, errors.New("TELEGRAM_MAX_SOURCE_BYTES must be between 1 and 524288000")
		}
		telegramMaxSourceBytes = parsed
	}
	komgaMappings, err := parseKomgaLibraryMappings(os.Getenv("KOMGA_LIBRARY_MAPPINGS"))
	if err != nil {
		return Config{}, err
	}
	komgaReadOnly, err := parseKomgaReadOnlyLibraries(os.Getenv("KOMGA_READONLY_LIBRARY_IDS"), komgaMappings)
	if err != nil {
		return Config{}, err
	}
	komgaBackupRoot := envOrDefault("KOMGA_EDIT_BACKUP_ROOT", "/app/komga-edit-backups")
	if err := validateKomgaBackupRoot(komgaBackupRoot, komgaMappings); err != nil {
		return Config{}, err
	}

	return Config{
		TelegramMaxSourceBytes: telegramMaxSourceBytes,
		SourceRetentionRoot:    envOrDefault("SOURCE_RETENTION_PATH", "/app/source-retention"),
		TelegramEnabled:        os.Getenv("TELEGRAM_ENABLED") == "true",
		TelegramPrivateRoot:    envOrDefault("TELEGRAM_PRIVATE_ROOT", "/app/telegram-private"),
		TelegramHelperPath:     envOrDefault("TELEGRAM_HELPER_PATH", "/app/tdl-auth-helper"),
		TelegramTDLPath:        envOrDefault("TELEGRAM_TDL_PATH", "/app/tdl"),
		KomgaLibraryMappings:   komgaMappings,
		KomgaReadOnlyLibraries: komgaReadOnly,
		KomgaEditBackupRoot:    komgaBackupRoot,
		PublicOrigin:           strings.TrimSpace(os.Getenv("APP_PUBLIC_ORIGIN")),
		AllowInsecureLoopback:  os.Getenv("ALLOW_INSECURE_LOOPBACK") == "true",
		Addr:                   addr,
		DatabaseURL:            databaseURL,
		ConsumerName:           consumerName,
		UpstreamBaseURL:        strings.TrimRight(strings.TrimSpace(os.Getenv("PYTHON_WEB_BASE_URL")), "/"),
		InternalToken:          internalToken,
		DownloadTimeout:        settings.Timeout,
		DownloadRetries:        settings.Retries,
		ImageConcurrency:       settings.ImageConcurrency,
		ReadTimeout:            15 * time.Second,
		WriteTimeout:           30 * time.Second,
		IdleTimeout:            60 * time.Second,
		ShutdownTimeout:        10 * time.Second,
	}, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
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
