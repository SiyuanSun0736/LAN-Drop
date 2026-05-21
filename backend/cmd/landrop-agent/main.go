package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/landrop/landrop/backend/internal/app"
	"github.com/landrop/landrop/backend/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	agent, err := app.New(config.Load())
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	if err := agent.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run agent: %v", err)
	}
}