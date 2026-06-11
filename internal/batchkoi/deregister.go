package batchkoi

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type DeregisterCmd struct {
	KeepCount    int   `name:"keep-count" help:"Keep only the N most recent ACTIVE revisions; deregister older ones."`
	KeepRevision []int `name:"keep-revision" help:"Revision number(s) to always keep (repeatable / comma-separated)."`
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
	if c.KeepCount <= 0 && len(c.KeepRevision) == 0 {
		return fmt.Errorf("deregister requires --keep-count and/or --keep-revision")
	}
	der, kept, err := app.applyRetention(name, c.KeepCount, c.KeepRevision)
	if err != nil {
		return err
	}
	return app.emit(&DeregisterResult{JobDefinitionName: name, Deregistered: der, Kept: kept})
}
