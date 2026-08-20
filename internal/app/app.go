package app

import (
	"context"
	"log/slog"
	"os"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/network"
	"openmir2/internal/storage"
	"openmir2/internal/world"
)

type App struct {
	configsDir string
	cfg        config.Config
	log        *slog.Logger
}

func New(configsDir string, cfg config.Config, log *slog.Logger) *App {
	return &App{configsDir: configsDir, cfg: cfg, log: log}
}

func (a *App) Run(ctx context.Context) error {
	bundle, report, err := data.LoadConfigsWithReport(a.configsDir)
	if err != nil {
		return err
	}
	for _, skipped := range report.Skipped {
		a.log.Warn("runtime config entry skipped", "kind", skipped.Kind, "file", skipped.File, "map", skipped.MapID, "id", skipped.ID, "reason", skipped.Reason)
	}
	gameplay, err := config.LoadGameplay(a.configsDir)
	if err != nil {
		return err
	}
	store, err := storage.Open(a.cfg.StoragePath)
	if err != nil {
		return err
	}
	w := world.New(bundle, store, gameplay)
	server := network.New(a.cfg.ServerName, a.cfg.Listeners, store, w, a.log)
	server.SetHitImpactDelay(time.Duration(gameplay.Combat.HitImpactDelayMS) * time.Millisecond)
	server.SetMonsterTickInterval(time.Duration(gameplay.Monster.TickMS) * time.Millisecond)
	a.log.Info("server ready", "name", a.cfg.ServerName, "configs_dir", a.configsDir, "storage", a.cfg.StoragePath)
	return server.Run(ctx)
}

func DefaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
