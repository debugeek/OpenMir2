package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"openmir2/internal/app"
	"openmir2/internal/config"
)

func main() {
	configPath := flag.String("config", "configs", "server config directory")
	storagePath := flag.String("storage-path", "", "override the config file's storage_path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *storagePath != "" {
		cfg.StoragePath = *storagePath
	}

	log := app.DefaultLogger()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.New(*configPath, cfg, log).Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
