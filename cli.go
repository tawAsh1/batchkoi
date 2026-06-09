package main

// CLI is the root command tree parsed by kong.
type CLI struct {
	Config string `name:"config" short:"c" default:"batchkoi.yml" help:"Path to the batchkoi config file (YAML)."`
	Output string `name:"output" short:"o" enum:"text,json" default:"text" help:"Output format: text or json."`
	Debug  bool   `name:"debug" help:"Enable verbose logging."`

	Render     RenderCmd     `cmd:"" help:"Render the job definition config to JSON."`
	Diff       DiffCmd       `cmd:"" help:"Diff the local config against the latest registered revision."`
	Register   RegisterCmd   `cmd:"" help:"Register a new job definition revision."`
	Deploy     DeployCmd     `cmd:"" help:"Register a new revision (only if changed) and prune old ones."`
	Run        RunCmd        `cmd:"" help:"Submit a job from the config and tail its CloudWatch logs."`
	Verify     VerifyCmd     `cmd:"" help:"Verify image, IAM roles and log group before deploying."`
	Status     StatusCmd     `cmd:"" help:"Show the registered revisions."`
	Deregister DeregisterCmd `cmd:"" help:"Deregister old revisions per --keep-count / --keep-revision."`
	Init       InitCmd       `cmd:"" help:"Generate a config from an existing job definition."`
	Version    VersionCmd    `cmd:"" help:"Show the batchkoi version."`
}
