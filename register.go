package main

import (
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

type RegisterCmd struct {
	DryRun bool `name:"dry-run" help:"Show the definition and the revision number a register would create, without registering."`
}

func (c *RegisterCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		return err
	}
	if c.DryRun {
		return c.dryRun(app)
	}
	res, err := app.register()
	if err != nil {
		return err
	}
	return app.emit(res)
}

// RegisterDryRunResult is the preview of a register.
type RegisterDryRunResult struct {
	JobDefinitionName string          `json:"jobDefinitionName"`
	DryRun            bool            `json:"dryRun"`
	NextRevision      int32           `json:"nextRevision"`
	JobDefinition     json.RawMessage `json:"jobDefinition"`
}

func (r RegisterDryRunResult) String() string {
	return fmt.Sprintf("would register %s:%d\n%sDRY RUN — nothing was changed",
		r.JobDefinitionName, r.NextRevision, r.JobDefinition)
}

// dryRun renders the local definition and reports the revision number a
// register would create, in the same canonical form diff uses.
func (c *RegisterCmd) dryRun(app *App) error {
	local, err := app.loadJobDefinition()
	if err != nil {
		return err
	}
	name := aws.ToString(local.JobDefinitionName)
	if name == "" {
		return fmt.Errorf("jobDefinitionName is empty in the rendered job definition")
	}
	all, err := app.listRevisions(name, "")
	if err != nil {
		return err
	}
	body, err := canonicalJSON(local)
	if err != nil {
		return err
	}
	return app.emit(&RegisterDryRunResult{
		JobDefinitionName: name,
		DryRun:            true,
		NextRevision:      maxRevision(all) + 1,
		JobDefinition:     json.RawMessage(body),
	})
}

// RegisterResult is the outcome of registering a new revision.
type RegisterResult struct {
	JobDefinitionName string `json:"jobDefinitionName"`
	Revision          int32  `json:"revision"`
	JobDefinitionArn  string `json:"jobDefinitionArn"`
	Status            string `json:"status"`
}

func (r RegisterResult) String() string {
	return fmt.Sprintf("registered %s:%d\n%s", r.JobDefinitionName, r.Revision, r.JobDefinitionArn)
}

// registerIfChanged registers the rendered definition only when it differs
// from the latest ACTIVE revision (deploy/run semantics). reg is nil when
// nothing changed; latest is the pre-existing latest revision (nil when the
// definition was never registered).
func (app *App) registerIfChanged(local *batch.RegisterJobDefinitionInput, name string) (reg *RegisterResult, latest *types.JobDefinition, err error) {
	latest, err = app.latestJobDefinition(name)
	if err != nil {
		return nil, nil, err
	}
	changed, _, err := computeDiff(local, latest, name)
	if err != nil {
		return nil, latest, err
	}
	if !changed {
		return nil, latest, nil
	}
	reg, err = app.register()
	return reg, latest, err
}

// register renders the local job definition and registers a new revision.
func (app *App) register() (*RegisterResult, error) {
	in, err := app.loadJobDefinition()
	if err != nil {
		return nil, err
	}
	out, err := app.batch.RegisterJobDefinition(app.ctx, in)
	if err != nil {
		return nil, fmt.Errorf("RegisterJobDefinition: %w", err)
	}
	return &RegisterResult{
		JobDefinitionName: aws.ToString(out.JobDefinitionName),
		Revision:          aws.ToInt32(out.Revision),
		JobDefinitionArn:  aws.ToString(out.JobDefinitionArn),
		Status:            "registered",
	}, nil
}
