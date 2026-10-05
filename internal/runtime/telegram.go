// Package runtime composes process-owned infrastructure shared by API and worker.
package runtime

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
	"github.com/ryancheng/telegram-downloader/internal/config"
	infra "github.com/ryancheng/telegram-downloader/internal/infra/telegram"
	store "github.com/ryancheng/telegram-downloader/internal/store/postgres/telegram"
)

func Telegram(ctx context.Context, pool *pgxpool.Pool, cfg config.Config) (*app.Service, error) {
	if !cfg.TelegramEnabled {
		return nil, nil
	}
	coordinator, err := infra.NewCoordinator(pool, cfg.TelegramPrivateRoot)
	if err != nil {
		return nil, err
	}
	bridge, err := infra.NewBridge(infra.BridgeOptions{HelperPath: cfg.TelegramHelperPath, TDLPath: cfg.TelegramTDLPath, PrivateRoot: cfg.TelegramPrivateRoot})
	if err != nil {
		return nil, err
	}
	service, err := app.NewService(store.NewStore(pool), coordinator, bridge, app.Options{MaxSourceBytes: cfg.TelegramMaxSourceBytes})
	if err != nil {
		return nil, err
	}
	go func() { <-ctx.Done(); service.Close() }()
	return service, nil
}
