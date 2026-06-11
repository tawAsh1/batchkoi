package batchkoi

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
)

type DiffCmd struct {
	Revision int  `name:"revision" aliases:"rev" help:"Diff against revision N instead of the latest ACTIVE one."`
	ExitCode bool `name:"exit-code" help:"Exit with code 2 when the definitions differ (errors stay exit 1, like lambroll / terraform plan)."`
}

func (c *DiffCmd) Run(app *App) error {
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

	var remote *types.JobDefinition
	if c.Revision > 0 {
		remote, err = app.jobDefinitionByRevision(name, c.Revision)
	} else {
		remote, err = app.latestJobDefinition(name)
	}
	if err != nil {
		return err
	}

	changed, unified, err := computeDiff(local, remote, name)
	if err != nil {
		return err
	}

	if app.cli.Output == "json" {
		out := map[string]any{
			"jobDefinitionName": name,
			"changed":           changed,
			"diff":              unified,
		}
		if remote != nil {
			out["remoteRevision"] = aws.ToInt32(remote.Revision)
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(app.out(), string(b))
	} else {
		if remote == nil {
			fmt.Fprintf(os.Stderr, "# %s is not registered yet — showing the full definition as new\n", name)
		}
		fmt.Fprint(app.out(), unified)
	}

	// 2 distinguishes "has differences" from runtime errors (exit 1) — the
	// lambroll / terraform plan -detailed-exitcode convention.
	if changed && c.ExitCode {
		return exitError{code: 2}
	}
	return nil
}

// computeDiff canonicalizes the local input and the remote revision (nil =
// not registered) and returns whether they differ plus a unified diff.
func computeDiff(local *batch.RegisterJobDefinitionInput, remote *types.JobDefinition, name string) (changed bool, unified string, err error) {
	localJSON, err := canonicalJSON(local)
	if err != nil {
		return false, "", err
	}
	fromLabel := name + " (remote)"
	remoteJSON := ""
	if remote != nil {
		fromLabel = fmt.Sprintf("%s:%d (remote)", name, aws.ToInt32(remote.Revision))
		remoteInput, err := remoteToInput(remote)
		if err != nil {
			return false, "", err
		}
		if remoteJSON, err = canonicalJSON(remoteInput); err != nil {
			return false, "", err
		}
	}
	if remoteJSON == localJSON {
		return false, "", nil
	}
	edits := myers.ComputeEdits(span.URIFromPath(fromLabel), remoteJSON, localJSON)
	return true, fmt.Sprint(gotextdiff.ToUnified(fromLabel, name+" (local)", remoteJSON, edits)), nil
}
