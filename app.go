package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/batch"
)

// App carries shared state across all commands.
type App struct {
	ctx    context.Context
	cli    *CLI
	config *Config
	awsCfg aws.Config
	batch  *batch.Client
}

// NewApp constructs the app. Config and AWS clients are loaded lazily via setup()
// so that commands like version/help work without a config file or credentials.
func NewApp(ctx context.Context, cli *CLI) (*App, error) {
	return &App{ctx: ctx, cli: cli}, nil
}

// setup loads batchkoi.yml and wires up the AWS clients. It is idempotent.
func (app *App) setup() error {
	if app.config != nil {
		return nil
	}
	cfg, err := LoadConfig(app.cli.Config)
	if err != nil {
		return err
	}
	app.config = cfg

	var opts []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(app.ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}
	app.awsCfg = awsCfg
	app.batch = batch.NewFromConfig(awsCfg)
	return nil
}
