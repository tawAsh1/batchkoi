package main

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
)

type DeployCmd struct {
	KeepCount    int   `name:"keep-count" help:"Keep only the N most recent ACTIVE revisions; deregister older ones. 0 = keep all."`
	KeepRevision []int `name:"keep-revision" help:"Revision number(s) to always keep (repeatable / comma-separated)."`
}

// DeployResult is the outcome of a deploy.
type DeployResult struct {
	JobDefinitionName string          `json:"jobDefinitionName"`
	NoChange          bool            `json:"noChange"`
	Registered        *RegisterResult `json:"registered"`
	Deregistered      []int32         `json:"deregistered"`
	Kept              []int32         `json:"kept"`
}

func (r DeployResult) String() string {
	var b strings.Builder
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
	res := &DeployResult{JobDefinitionName: name}

	// Register only if the rendered definition differs from the latest revision.
	latest, err := app.latestJobDefinition(name)
	if err != nil {
		return err
	}
	changed := true
	if latest != nil {
		localJSON, err := canonicalJSON(local)
		if err != nil {
			return err
		}
		remoteInput, err := remoteToInput(latest)
		if err != nil {
			return err
		}
		remoteJSON, err := canonicalJSON(remoteInput)
		if err != nil {
			return err
		}
		changed = localJSON != remoteJSON
	}

	if changed {
		reg, err := app.register()
		if err != nil {
			return err
		}
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
