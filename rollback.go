package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
)

type RollbackCmd struct {
	DryRun bool `name:"dry-run" help:"Show what would be deregistered without doing it."`
}

// RollbackResult is the outcome of a rollback.
type RollbackResult struct {
	JobDefinitionName string `json:"jobDefinitionName"`
	DryRun            bool   `json:"dryRun,omitempty"`
	Deregistered      int32  `json:"deregistered"`
	NowLatest         int32  `json:"nowLatest"`
}

func (r RollbackResult) String() string {
	if r.DryRun {
		return fmt.Sprintf("would deregister %s:%d — latest would become %s:%d\nDRY RUN — nothing was changed",
			r.JobDefinitionName, r.Deregistered, r.JobDefinitionName, r.NowLatest)
	}
	return fmt.Sprintf("deregistered %s:%d — latest is now %s:%d",
		r.JobDefinitionName, r.Deregistered, r.JobDefinitionName, r.NowLatest)
}

// Run deregisters the latest ACTIVE revision so the previous one becomes
// latest again. Jobs submitted by bare name (or run --rev latest) resolve to
// the highest ACTIVE revision, so this is the whole rollback story for Batch.
func (c *RollbackCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		return err
	}
	local, err := app.loadJobDefinition()
	if err != nil {
		return err
	}
	name := aws.ToString(local.JobDefinitionName)
	if name == "" {
		return fmt.Errorf("jobDefinitionName is empty in the rendered job definition")
	}

	revs, err := app.listActiveRevisions(name)
	if err != nil {
		return err
	}
	if len(revs) < 2 {
		return fmt.Errorf("cannot roll back %s: need at least 2 ACTIVE revisions (have %d)", name, len(revs))
	}

	res := &RollbackResult{
		JobDefinitionName: name,
		DryRun:            c.DryRun,
		Deregistered:      aws.ToInt32(revs[0].Revision),
		NowLatest:         aws.ToInt32(revs[1].Revision),
	}
	if !c.DryRun {
		if _, err := app.batch.DeregisterJobDefinition(app.ctx, &batch.DeregisterJobDefinitionInput{
			JobDefinition: revs[0].JobDefinitionArn,
		}); err != nil {
			return fmt.Errorf("DeregisterJobDefinition %s:%d: %w", name, res.Deregistered, err)
		}
	}
	return app.emit(res)
}
