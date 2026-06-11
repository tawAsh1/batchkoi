package batchkoi

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// App carries shared state across all commands.
type App struct {
	ctx    context.Context
	cli    *CLI
	config *Config
	awsCfg aws.Config
	batch  *batch.Client
	logs   *cloudwatchlogs.Client

	identity *sts.GetCallerIdentityOutput // cached by callerIdentity()
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
	if err := exportEnvFiles(app.cli.Envfile); err != nil {
		return err
	}
	cfg, err := LoadConfig(app.cli.Config)
	if err != nil {
		return err
	}
	app.config = cfg
	return app.setupAWS(cfg.Region)
}

// setupAWS wires up the AWS clients. Called by setup(), and directly by
// commands like init that must run before a config file exists.
func (app *App) setupAWS(region string) error {
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(app.ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}
	app.awsCfg = awsCfg
	app.batch = batch.NewFromConfig(awsCfg)
	app.logs = cloudwatchlogs.NewFromConfig(awsCfg)
	return nil
}

// callerIdentity returns the STS caller identity, cached per process.
func (app *App) callerIdentity() (*sts.GetCallerIdentityOutput, error) {
	if app.identity != nil {
		return app.identity, nil
	}
	out, err := sts.NewFromConfig(app.awsCfg).GetCallerIdentity(app.ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("sts GetCallerIdentity: %w", err)
	}
	app.identity = out
	return out, nil
}
