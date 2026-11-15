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
	publicHost := flag.String("public-host", os.Getenv("OPENMIR2_PUBLIC_HOST"), "host advertised to clients for select and game connections")
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

	application := app.New(*configPath, cfg, log)
	application.SetPublicHost(*publicHost)
	if err := application.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
