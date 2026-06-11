package batchkoi

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// batchAPI is the slice of the AWS Batch client that batchkoi uses,
// so tests can substitute a fake.
type batchAPI interface {
	DescribeJobDefinitions(context.Context, *batch.DescribeJobDefinitionsInput, ...func(*batch.Options)) (*batch.DescribeJobDefinitionsOutput, error)
	RegisterJobDefinition(context.Context, *batch.RegisterJobDefinitionInput, ...func(*batch.Options)) (*batch.RegisterJobDefinitionOutput, error)
	DeregisterJobDefinition(context.Context, *batch.DeregisterJobDefinitionInput, ...func(*batch.Options)) (*batch.DeregisterJobDefinitionOutput, error)
	SubmitJob(context.Context, *batch.SubmitJobInput, ...func(*batch.Options)) (*batch.SubmitJobOutput, error)
	DescribeJobs(context.Context, *batch.DescribeJobsInput, ...func(*batch.Options)) (*batch.DescribeJobsOutput, error)
	DescribeJobQueues(context.Context, *batch.DescribeJobQueuesInput, ...func(*batch.Options)) (*batch.DescribeJobQueuesOutput, error)
}

// logsAPI is the slice of the CloudWatch Logs client that batchkoi uses.
type logsAPI interface {
	GetLogEvents(context.Context, *cloudwatchlogs.GetLogEventsInput, ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetLogEventsOutput, error)
	DescribeLogGroups(context.Context, *cloudwatchlogs.DescribeLogGroupsInput, ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error)
}

// App carries shared state across all commands.
type App struct {
	ctx    context.Context
	cli    *CLI
	config *Config
	awsCfg aws.Config
	batch  batchAPI
	logs   logsAPI
	stdout io.Writer // result output (emit); os.Stdout outside tests

	identity *sts.GetCallerIdentityOutput // cached by callerIdentity()
}

// NewApp constructs the app. Config and AWS clients are loaded lazily via setup()
// so that commands like version/help work without a config file or credentials.
func NewApp(ctx context.Context, cli *CLI) *App {
	return &App{ctx: ctx, cli: cli, stdout: os.Stdout}
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
