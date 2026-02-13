package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aliebadimehr/telegram-uploader-bot/internal/bot"
)

func main() {
	configPath := "config.yaml"
	if env := os.Getenv("CONFIG_PATH"); env != "" {
		configPath = env
	}

	uploader, err := bot.New(configPath)
	if err != nil {
		log.Fatalf("failed to initialize bot: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := uploader.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("bot stopped: %v", err)
	}
}
