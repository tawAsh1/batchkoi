package main

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

type DeployCmd struct {
	KeepCount    int   `name:"keep-count" help:"Keep only the N most recent ACTIVE revisions; deregister older ones. 0 = keep all."`
	KeepRevision []int `name:"keep-revision" help:"Revision number(s) to always keep (repeatable / comma-separated)."`
	DryRun       bool  `name:"dry-run" help:"Show what would be registered and deregistered without changing anything."`
}

// DeployResult is the outcome of a deploy (or a dry-run preview of one).
type DeployResult struct {
	JobDefinitionName string          `json:"jobDefinitionName"`
	DryRun            bool            `json:"dryRun,omitempty"`
	NoChange          bool            `json:"noChange"`
	Registered        *RegisterResult `json:"registered,omitempty"`
	NextRevision      int32           `json:"nextRevision,omitempty"` // dry-run: revision a register would create
	Deregistered      []int32         `json:"deregistered"`
	Kept              []int32         `json:"kept"`
	Diff              string          `json:"diff,omitempty"` // dry-run only
}

func (r DeployResult) String() string {
	var b strings.Builder
	if r.DryRun {
		if r.NoChange {
			fmt.Fprintf(&b, "no changes (%s)\n", r.JobDefinitionName)
		} else {
			fmt.Fprintf(&b, "would register %s:%d\n", r.JobDefinitionName, r.NextRevision)
			b.WriteString(r.Diff)
		}
		if len(r.Deregistered) > 0 {
			fmt.Fprintf(&b, "would deregister: %s\n", joinInts(r.Deregistered))
		}
		if len(r.Kept) > 0 {
			fmt.Fprintf(&b, "would keep: %s\n", joinInts(r.Kept))
		}
		fmt.Fprint(&b, "DRY RUN — nothing was changed")
		return b.String()
	}
	if r.NoChange {
		fmt.Fprintf(&b, "no changes (%s)\n", r.JobDefinitionName)
	} else if r.Registered != nil {
		fmt.Fprintf(&b, "registered %s:%d\n", r.Registered.JobDefinitionName, r.Registered.Revision)
	}
	if len(r.Deregistered) > 0 {
		fmt.Fprintf(&b, "deregistered: %s\n", joinInts(r.Deregistered))
	}
	if len(r.Kept) > 0 {
		fmt.Fprintf(&b, "kept: %s\n", joinInts(r.Kept))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *DeployCmd) Run(app *App) error {
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
	res := &DeployResult{JobDefinitionName: name, DryRun: c.DryRun}

	if c.DryRun {
		return c.dryRun(app, local, res)
	}

	// Register only if the rendered definition differs from the latest revision.
	reg, _, err := app.registerIfChanged(local, name)
	if err != nil {
		return err
	}
	if reg != nil {
		res.Registered = reg
	} else {
		res.NoChange = true
	}

	// Apply the retention policy (only when requested).
	if c.KeepCount > 0 || len(c.KeepRevision) > 0 {
		der, kept, err := app.applyRetention(name, c.KeepCount, c.KeepRevision)
		if err != nil {
			return err
		}
		res.Deregistered = der
		res.Kept = kept
	}

	return app.emit(res)
}

// dryRun previews a deploy: whether a new revision would be registered (and
// its number), and which revisions the retention policy would deregister —
// counting the would-be new revision, exactly as the real deploy would.
func (c *DeployCmd) dryRun(app *App, local *batch.RegisterJobDefinitionInput, res *DeployResult) error {
	// Fetch INACTIVE revisions too: Batch never reuses revision numbers, so
	// the next revision is max(all)+1, not max(ACTIVE)+1.
	all, err := app.listRevisions(res.JobDefinitionName, "")
	if err != nil {
		return err
	}
	var latest *types.JobDefinition
	var actives []int32
	for i := range all {
		jd := &all[i]
		if aws.ToString(jd.Status) == "ACTIVE" {
			if latest == nil {
				latest = jd
			}
			actives = append(actives, aws.ToInt32(jd.Revision))
		}
	}

	changed, unified, err := computeDiff(local, latest, res.JobDefinitionName)
	if err != nil {
		return err
	}
	res.NoChange = !changed
	if changed {
		res.NextRevision = maxRevision(all) + 1
		res.Diff = unified
	}

	if c.KeepCount > 0 || len(c.KeepRevision) > 0 {
		if changed {
			actives = append([]int32{res.NextRevision}, actives...)
		}
		res.Deregistered, res.Kept = computeRetention(actives, c.KeepCount, c.KeepRevision)
	}
	return app.emit(res)
}

// applyRetention deregisters ACTIVE revisions that fall outside the keep policy.
func (app *App) applyRetention(name string, keepCount int, keepRevision []int) (deregistered, kept []int32, err error) {
	revs, err := app.listActiveRevisions(name)
	if err != nil {
		return nil, nil, err
	}
	nums := make([]int32, len(revs))
	arnByRev := make(map[int32]string, len(revs))
	for i, jd := range revs {
		n := aws.ToInt32(jd.Revision)
		nums[i] = n
		arnByRev[n] = aws.ToString(jd.JobDefinitionArn)
	}
	deregistered, kept = computeRetention(nums, keepCount, keepRevision)
	for _, rev := range deregistered {
		if _, err := app.batch.DeregisterJobDefinition(app.ctx, &batch.DeregisterJobDefinitionInput{
			JobDefinition: aws.String(arnByRev[rev]),
		}); err != nil {
			return nil, nil, fmt.Errorf("DeregisterJobDefinition %s:%d: %w", name, rev, err)
		}
	}
	return deregistered, kept, nil
}

// computeRetention decides which revisions to keep vs deregister. revisions must
// be sorted newest-first. keepCount <= 0 keeps everything; the revisions in
// keepRevision are always protected.
func computeRetention(revisions []int32, keepCount int, keepRevision []int) (deregister, kept []int32) {
	protect := make(map[int32]bool, len(keepRevision))
	for _, r := range keepRevision {
		protect[int32(r)] = true
	}
	for i, rev := range revisions {
		keep := keepCount <= 0 || i < keepCount || protect[rev]
		if keep {
			kept = append(kept, rev)
		} else {
			deregister = append(deregister, rev)
		}
	}
	return deregister, kept
}
