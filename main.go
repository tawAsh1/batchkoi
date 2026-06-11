package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/tawAsh1/batchkoi/internal/batchkoi"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

// resolveVersion falls back to the module version from build info when no
// ldflags were set — `go install github.com/tawAsh1/batchkoi@v0.1.0` embeds
// "v0.1.0" there, so those installs report a real version (and the config's
// required_version check applies to them) instead of "dev".
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func main() {
	// Ctrl-C / SIGTERM cancel the context so in-flight AWS calls and the
	// `run` log tail stop cleanly. After the first signal, stop() restores
	// default handling so a second Ctrl-C force-kills.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	os.Exit(batchkoi.Run(ctx, resolveVersion()))
}
