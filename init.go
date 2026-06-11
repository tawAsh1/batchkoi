package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

type InitCmd struct {
	JobDefinition string `name:"job-definition" aliases:"jd" required:"" help:"Job definition to import: name, name:revision, or ARN."`
	Jsonnet       bool   `name:"jsonnet" help:"Write jobdef.jsonnet instead of jobdef.json."`
	JobQueue      string `name:"job-queue" help:"Also write job_queue into the generated config."`
	Force         bool   `name:"force" help:"Overwrite existing files."`
}

// InitResult is the outcome of an init.
type InitResult struct {
	JobDefinitionName string   `json:"jobDefinitionName"`
	Revision          int32    `json:"revision"`
	Files             []string `json:"files"`
}

func (r InitResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "imported %s:%d\n", r.JobDefinitionName, r.Revision)
	for _, f := range r.Files {
		fmt.Fprintf(&b, "wrote %s\n", f)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Run fetches an existing job definition from AWS and reverse-generates the
// batchkoi files: the config (at -c, default batchkoi.yml) plus jobdef.json
// (or .jsonnet). The job definition body is the same canonical form diff
// uses, so `batchkoi diff` right after init shows no changes.
func (c *InitCmd) Run(app *App) error {
	jobdefFile := "jobdef.json"
	if c.Jsonnet {
		jobdefFile = "jobdef.jsonnet" // JSON is valid Jsonnet; start from here
	}
	configPath := app.cli.Config
	jobdefPath := filepath.Join(filepath.Dir(configPath), jobdefFile)
	if !c.Force {
		for _, path := range []string{configPath, jobdefPath} {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists (use --force to overwrite)", path)
			}
		}
	}

	// No config file exists yet, so wire AWS directly from the default chain
	// (AWS_REGION / profile).
	if err := app.setupAWS(""); err != nil {
		return err
	}

	jd, err := app.findJobDefinition(c.JobDefinition)
	if err != nil {
		return err
	}
	in, err := remoteToInput(jd)
	if err != nil {
		return err
	}
	body, err := canonicalJSON(in)
	if err != nil {
		return err
	}

	var cfg strings.Builder
	fmt.Fprintf(&cfg, "region: %s\n", app.awsCfg.Region)
	fmt.Fprintf(&cfg, "job_definition: %s\n", jobdefFile)
	if c.JobQueue != "" {
		fmt.Fprintf(&cfg, "job_queue: %s\n", c.JobQueue)
	}

	files := map[string]string{configPath: cfg.String(), jobdefPath: body}
	res := &InitResult{
		JobDefinitionName: aws.ToString(jd.JobDefinitionName),
		Revision:          aws.ToInt32(jd.Revision),
	}
	for _, path := range []string{configPath, jobdefPath} {
		if err := os.WriteFile(path, []byte(files[path]), 0644); err != nil {
			return err
		}
		res.Files = append(res.Files, path)
	}
	return app.emit(res)
}

// findJobDefinition resolves a name, name:revision, or ARN to one revision.
// A bare name resolves to the latest ACTIVE revision.
func (app *App) findJobDefinition(spec string) (*types.JobDefinition, error) {
	if strings.HasPrefix(spec, "arn:") {
		out, err := app.batch.DescribeJobDefinitions(app.ctx, &batch.DescribeJobDefinitionsInput{
			JobDefinitions: []string{spec},
		})
		if err != nil {
			return nil, fmt.Errorf("DescribeJobDefinitions %s: %w", spec, err)
		}
		if len(out.JobDefinitions) == 0 {
			return nil, fmt.Errorf("job definition %s not found", spec)
		}
		return &out.JobDefinitions[0], nil
	}
	if name, rev, ok := strings.Cut(spec, ":"); ok {
		n, err := strconv.Atoi(rev)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid revision in %q (use name, name:N, or an ARN)", spec)
		}
		return app.jobDefinitionByRevision(name, n)
	}
	jd, err := app.latestJobDefinition(spec)
	if err != nil {
		return nil, err
	}
	if jd == nil {
		return nil, fmt.Errorf("no ACTIVE revision found for %s", spec)
	}
	return jd, nil
}
