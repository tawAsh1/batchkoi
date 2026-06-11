package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/tawAsh1/batchkoi/internal/batchkoi"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// Ctrl-C / SIGTERM cancel the context so in-flight AWS calls and the
	// `run` log tail stop cleanly. After the first signal, stop() restores
	// default handling so a second Ctrl-C force-kills.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	os.Exit(batchkoi.Run(ctx, version))
}
