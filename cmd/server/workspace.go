package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ryancheng/telegram-downloader/internal/app/adminauth"
	"github.com/ryancheng/telegram-downloader/internal/app/extractionrules"
	"github.com/ryancheng/telegram-downloader/internal/app/komgaedit"
	appmetadata "github.com/ryancheng/telegram-downloader/internal/app/metadata"
	"github.com/ryancheng/telegram-downloader/internal/app/metadataextract"
	"github.com/ryancheng/telegram-downloader/internal/app/metadatasearch"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	apptaskcore "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	"github.com/ryancheng/telegram-downloader/internal/config"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/httpapi"
	"github.com/ryancheng/telegram-downloader/internal/httpui"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/infra/metadataproviders"
	"github.com/ryancheng/telegram-downloader/internal/modelapi"
	appruntime "github.com/ryancheng/telegram-downloader/internal/runtime"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
	pgadmin "github.com/ryancheng/telegram-downloader/internal/store/postgres/adminauth"
	pgrules "github.com/ryancheng/telegram-downloader/internal/store/postgres/extractionrules"
	pgkomga "github.com/ryancheng/telegram-downloader/internal/store/postgres/komgaedit"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/metadatadoc"
	pgmigrations "github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
	pgsources "github.com/ryancheng/telegram-downloader/internal/store/postgres/sourcesettings"
	pgtaskcore "github.com/ryancheng/telegram-downloader/internal/store/postgres/taskcore"
)

func buildWorkspaceRouter(ctx context.Context, pool *pgxpool.Pool, cfg config.Config) (http.Handler, error) {
	adminRepo := pgadmin.New(pool)
	admin := adminauth.NewService(adminRepo, adminauth.Options{})
	if _, err := adminRepo.Account(ctx); errors.Is(err, adminauth.ErrNotFound) {
		password, err := credentials.LoadSecret(os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"), os.Getenv("ADMIN_BOOTSTRAP_PASSWORD_FILE"))
		if err != nil {
			return nil, err
		}
		if err := admin.Bootstrap(ctx, password); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	encodedKey, err := credentials.LoadSecret(os.Getenv("SOURCE_SETTINGS_MASTER_KEY"), os.Getenv("SOURCE_SETTINGS_MASTER_KEY_FILE"))
	if err != nil {
		return nil, err
	}
	vault, err := credentials.NewVaultFromEncoded(encodedKey)
	if err != nil {
		return nil, err
	}

	metadata := appmetadata.NewService(metadatadoc.NewStore(pool))
	if _, err := metadata.Schema(ctx); err != nil {
		return nil, err
	}
	providers := metadataproviders.New()
	sourceSettings := sourcesettings.NewService(pgsources.New(pool), vault, providers, modelapi.NewClient(), modelTestContract{metadata: metadata})
	komgaConnection := komgaedit.NewConnectionService(pgkomga.NewConnectionStore(pool), vault, cfg.AllowInsecureLoopback)
	komgaCatalog := komgaedit.NewCatalogService(komgaConnection, cfg.KomgaLibraryMappings, cfg.KomgaReadOnlyLibraries)
	komgaEditor, err := komgaedit.NewEditService(komgaCatalog, pgkomga.NewOperationStore(pool), metadata, cfg.KomgaEditBackupRoot)
	if err != nil {
		return nil, err
	}
	reconcileCtx, cancelReconcile := context.WithTimeout(ctx, 10*time.Second)
	if _, err := komgaEditor.ReconcilePending(reconcileCtx, 100); err != nil {
		// A pending edit remains available for on-demand reconciliation through
		// its operation endpoint. Avoid logging paths, backups or field values.
		log.Print("Komga edit startup reconciliation incomplete; pending operations remain available")
	}
	cancelReconcile()
	taskCore := apptaskcore.NewService(pgtaskcore.NewStore(pool), apptaskcore.Config{LeaseTTL: 30 * time.Second, MaxAttempts: 3, MetadataNormalizer: metadata, MetadataEncoder: metadata})
	settings := postgres.NewSettingsStore(pool)
	telegram, err := appruntime.Telegram(ctx, pool, cfg)
	if err != nil {
		return nil, err
	}
	options := buildLegacyRouterOptions(cfg, postgres.NewUploadTaskStore(pool))
	options.TaskCoreService, options.SettingsProvider = taskCore, settings
	options.KomgaDelivery = komgaedit.NewDeliveryService(komgaCatalog, options.KomgaRootDir)
	options.ReadyzChecker = func(ctx context.Context) (bool, error) {
		n, err := pgmigrations.PendingCount(ctx, pool)
		return n == 0, err
	}
	auth, err := httpapi.NewAdminAuth(admin, httpapi.AdminAuthOptions{PublicOrigin: cfg.PublicOrigin, AllowInsecureLoopback: cfg.AllowInsecureLoopback, PublicStaticPaths: publicWorkspaceAssets(resolveUIStaticDir())})
	if err != nil {
		return nil, err
	}
	r := chi.NewRouter()
	r.Use(auth.Middleware)
	r.Use(workspaceDeadlines)
	auth.RegisterRoutes(r)
	httpapi.RegisterClientContract(r)
	httpapi.RegisterMetadata(r, metadata)
	httpapi.NewSourceSettings(sourceSettings).RegisterRoutes(r)
	httpapi.NewDownloadSettingsHandler(settings).RegisterRoutes(r)
	httpapi.NewKomgaHandler(komgaConnection, komgaCatalog).RegisterRoutes(r)
	httpapi.NewKomgaEditHandler(komgaEditor).RegisterRoutes(r)
	httpapi.RegisterExtractionRules(r, extractionrules.NewService(pgrules.New(pool), metadata))
	httpapi.RegisterMetadataExtract(r, metadataextract.NewService(metadata, sourceSettings))
	httpapi.NewMetadataSearchHandler(metadatasearch.NewService(sourceSettings, metadata, providers)).RegisterRoutes(r)
	// Avoid a typed-nil interface: disabled local installations remain explicit.
	var telegramAccount httpapi.TelegramAccountService
	if telegram != nil {
		telegramAccount = telegram
	}
	httpapi.NewTelegram(telegramAccount).RegisterRoutes(r)
	httpapi.RegisterTelegramDownloads(r, telegram, taskCore, settings)
	httpui.RegisterRoutesWithConfig(r, buildUIConfig(cfg, settings))
	httpv2.RegisterSettings(r, httpv2.NewSettingsHandler(settings))
	r.Mount("/", httpapi.NewRouterWithOptions(postgres.NewStore(pool), options))
	return r, nil
}

func publicWorkspaceAssets(dir string) []string {
	paths := []string{"/static/style.css"}
	// Vite emits shared chunks below dist/assets; login needs those exact files too.
	_ = filepath.WalkDir(filepath.Join(dir, "dist"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".js") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err == nil {
			paths = append(paths, "/static/"+filepath.ToSlash(rel))
		}
		return nil
	})
	return paths
}

func workspaceDeadlines(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/telegram/") && !strings.HasSuffix(r.URL.Path, "/events") {
			if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(65 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				http.Error(w, "request unavailable", 503)
				return
			}
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/settings/ai/test" || r.URL.Path == "/api/metadata/extract") {
			if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(130 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				http.Error(w, "request unavailable", 503)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
