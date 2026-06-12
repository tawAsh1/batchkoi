package batchkoi

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
)

type DeregisterCmd struct {
	KeepCount    int   `name:"keep-count" help:"Keep only the N most recent ACTIVE revisions; deregister older ones."`
	KeepRevision []int `name:"keep-revision" help:"Revision number(s) to protect from --keep-count pruning (repeatable / comma-separated)."`
	Revision     []int `name:"revision" aliases:"rev" help:"Deregister exactly these revision number(s) (repeatable / comma-separated), instead of --keep-count pruning."`
}

// DeregisterResult is the outcome of a deregister.
type DeregisterResult struct {
	JobDefinitionName string  `json:"jobDefinitionName"`
	Deregistered      []int32 `json:"deregistered"`
	Kept              []int32 `json:"kept"`
}

func (r DeregisterResult) String() string {
	var b strings.Builder
	if len(r.Deregistered) > 0 {
		fmt.Fprintf(&b, "deregistered: %s\n", joinInts(r.Deregistered))
	} else {
		fmt.Fprint(&b, "deregistered: (none)\n")
	}
	fmt.Fprintf(&b, "kept: %s", joinInts(r.Kept))
	return b.String()
}

func (c *DeregisterCmd) Run(app *App) error {
	switch {
	case len(c.Revision) > 0 && (c.KeepCount > 0 || len(c.KeepRevision) > 0):
		return fmt.Errorf("--rev cannot be combined with --keep-count / --keep-revision")
	case len(c.Revision) == 0 && c.KeepCount <= 0:
		return fmt.Errorf("pass --keep-count N (prune old revisions) or --rev N (deregister specific revisions)")
	}
	warnKeepCountOne(c.KeepCount)
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

	var der, kept []int32
	if len(c.Revision) > 0 {
		der, kept, err = app.deregisterRevisions(name, c.Revision)
	} else {
		der, kept, err = app.applyRetention(name, c.KeepCount, c.KeepRevision)
	}
	if err != nil {
		return err
	}
	return app.emit(&DeregisterResult{JobDefinitionName: name, Deregistered: der, Kept: kept})
}

// deregisterRevisions deregisters exactly the given revisions of name. Every
// target must currently be ACTIVE — a typo'd or already-INACTIVE revision is
// an error before anything is deregistered. kept is the ACTIVE revisions that
// remain.
func (app *App) deregisterRevisions(name string, revisions []int) (deregistered, kept []int32, err error) {
	actives, err := app.listActiveRevisions(name)
	if err != nil {
		return nil, nil, err
	}
	arnByRev := make(map[int32]string, len(actives))
	for _, jd := range actives {
		arnByRev[aws.ToInt32(jd.Revision)] = aws.ToString(jd.JobDefinitionArn)
	}

	targets := make(map[int32]bool, len(revisions))
	for _, r := range revisions {
		rev := int32(r)
		if arnByRev[rev] == "" {
			return nil, nil, fmt.Errorf("revision %s:%d is not ACTIVE (already deregistered, or never existed)", name, r)
		}
		targets[rev] = true
	}

	deregistered, kept = []int32{}, []int32{}
	for _, jd := range actives { // newest-first, like the retention path
		rev := aws.ToInt32(jd.Revision)
		if !targets[rev] {
			kept = append(kept, rev)
			continue
		}
		if _, err := app.batch.DeregisterJobDefinition(app.ctx, &batch.DeregisterJobDefinitionInput{
			JobDefinition: jd.JobDefinitionArn,
		}); err != nil {
			return nil, nil, fmt.Errorf("DeregisterJobDefinition %s:%d: %w", name, rev, err)
		}
		deregistered = append(deregistered, rev)
	}
	return deregistered, kept, nil
}
