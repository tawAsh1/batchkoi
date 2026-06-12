package batchkoi

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

type RegisterCmd struct {
	Revision int  `name:"revision" aliases:"rev" help:"Register a copy of existing revision N as a new revision (roll-forward), instead of the local file."`
	DryRun   bool `name:"dry-run" help:"Show the definition and the revision number a register would create, without registering."`
}

func (c *RegisterCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		return err
	}
	in, name, err := c.loadInput(app)
	if err != nil {
		return err
	}
	if c.DryRun {
		return c.dryRun(app, in, name)
	}
	if c.Revision > 0 {
		fmt.Fprintf(os.Stderr, "registering a copy of %s:%d\n", name, c.Revision)
	}
	res, err := app.register(in)
	if err != nil {
		return err
	}
	return app.emit(res)
}

// loadInput returns what register would send: the rendered local file, or —
// with --rev N — a copy of that existing revision fetched from AWS (the name
// still comes from the local config, so --rev can't drift to another
// definition). Re-registering an old revision makes it the new latest, which
// is the roll-forward complement to rollback.
func (c *RegisterCmd) loadInput(app *App) (*batch.RegisterJobDefinitionInput, string, error) {
	local, err := app.loadJobDefinition()
	if err != nil {
		return nil, "", err
	}
	name := aws.ToString(local.JobDefinitionName)
	if name == "" {
		return nil, "", fmt.Errorf("jobDefinitionName is empty in the rendered job definition")
	}
	if c.Revision <= 0 {
		return local, name, nil
	}
	jd, err := app.jobDefinitionByRevision(name, c.Revision)
	if err != nil {
		return nil, "", err
	}
	in, err := remoteToInput(jd)
	if err != nil {
		return nil, "", err
	}
	return in, name, nil
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

// dryRun reports the definition a register would send (the local file, or
// the --rev copy) and the revision number it would create, in the same
// canonical form diff uses.
func (c *RegisterCmd) dryRun(app *App, in *batch.RegisterJobDefinitionInput, name string) error {
	all, err := app.listRevisions(name, "")
	if err != nil {
		return err
	}
	body, err := canonicalJSON(in)
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
	reg, err = app.register(local)
	return reg, latest, err
}

// register registers in as a new revision. It takes the already-rendered
// input (rather than rendering again) so the definition that was diffed is
// byte-for-byte the one that gets registered.
func (app *App) register(in *batch.RegisterJobDefinitionInput) (*RegisterResult, error) {
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
