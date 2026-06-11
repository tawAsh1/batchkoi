package batchkoi

import (
	"context"
	"errors"

	"github.com/alecthomas/kong"
)

// version is the binary version, set by Run. Used by the version command and
// the config's required_version check.
var version = "dev"

// exitError makes a command exit with a specific code without printing an
// error message (e.g. diff --exit-code, mirroring git diff).
type exitError struct{ code int }

func (e exitError) Error() string { return "" }

// Run parses the command line and executes the selected command, returning
// the process exit code. Errors are printed (and exit non-zero) via kong.
func Run(ctx context.Context, ver string) int {
	version = ver

	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("batchkoi"),
		kong.Description("batchkoi \U0001F3A3 — a minimal deployment tool for AWS Batch job definitions.\nバッチこい！"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.DefaultEnvars("BATCHKOI"), // every flag falls back to BATCHKOI_*, like lambroll
	)

	err := kctx.Run(NewApp(ctx, &cli))
	var ee exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	kctx.FatalIfErrorf(err)
	return 0
}

// CLI is the root command tree parsed by kong.
type CLI struct {
	Config  string            `name:"config" short:"c" default:"batchkoi.yml" help:"Path to the batchkoi config file (YAML)."`
	Output  string            `name:"output" short:"o" enum:"text,json" default:"text" help:"Output format: text or json."`
	ExtStr  map[string]string `name:"ext-str" help:"Jsonnet external string variable (KEY=VALUE, repeatable)."`
	ExtCode map[string]string `name:"ext-code" help:"Jsonnet external code variable (KEY=VALUE, repeatable)."`
	Envfile []string          `name:"envfile" help:"Environment file(s) to export before rendering (repeatable)."`
	Debug   bool              `name:"debug" help:"Enable verbose logging."`

	Render     RenderCmd     `cmd:"" help:"Render the job definition config to JSON."`
	Diff       DiffCmd       `cmd:"" help:"Diff the local config against a registered revision."`
	Register   RegisterCmd   `cmd:"" help:"Register a new job definition revision."`
	Deploy     DeployCmd     `cmd:"" help:"Register a new revision (only if changed) and prune old ones."`
	Run        RunCmd        `cmd:"" help:"Submit a job from the config and tail its CloudWatch logs."`
	Verify     VerifyCmd     `cmd:"" help:"Verify job queue, IAM roles, image and log group before deploying."`
	Revisions  RevisionsCmd  `cmd:"" help:"List the registered revisions."`
	Rollback   RollbackCmd   `cmd:"" help:"Deregister the latest revision so the previous one becomes latest."`
	Deregister DeregisterCmd `cmd:"" help:"Deregister old revisions per --keep-count / --keep-revision."`
	Init       InitCmd       `cmd:"" help:"Generate batchkoi.yml + a job definition file from an existing one on AWS."`
	Version    VersionCmd    `cmd:"" help:"Show the batchkoi version."`
}
