package main

import (
	"context"
	"errors"
	"os"

	"github.com/alecthomas/kong"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

// exitError makes a command exit with a specific code without printing an
// error message (e.g. diff --exit-code, mirroring git diff).
type exitError struct{ code int }

func (e exitError) Error() string { return "" }

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("batchkoi"),
		kong.Description("batchkoi \U0001F3A3 — a minimal deployment tool for AWS Batch job definitions.\nバッチこい！"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.DefaultEnvars("BATCHKOI"), // every flag falls back to BATCHKOI_*, like lambroll
	)

	app, err := NewApp(context.Background(), &cli)
	kctx.FatalIfErrorf(err)

	err = kctx.Run(app)
	var ee exitError
	if errors.As(err, &ee) {
		os.Exit(ee.code)
	}
	kctx.FatalIfErrorf(err)
}
