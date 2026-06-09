package main

import (
	"context"

	"github.com/alecthomas/kong"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("batchkoi"),
		kong.Description("batchkoi \U0001F3A3 — a minimal deployment tool for AWS Batch job definitions.\nバッチこい！"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)

	app, err := NewApp(context.Background(), &cli)
	kctx.FatalIfErrorf(err)

	kctx.FatalIfErrorf(kctx.Run(app))
}
