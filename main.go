package main

import (
	"context"
	"os"

	"github.com/tawAsh1/batchkoi/internal/batchkoi"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(batchkoi.Run(context.Background(), version))
}
