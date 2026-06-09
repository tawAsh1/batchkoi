package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
)

type DiffCmd struct{}

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
	localJSON, err := canonicalJSON(local)
	if err != nil {
		return err
	}

	remote, err := app.latestJobDefinition(name)
	if err != nil {
		return err
	}

	fromLabel := name + " (remote)"
	remoteJSON := ""
	if remote != nil {
		fromLabel = fmt.Sprintf("%s:%d (remote)", name, aws.ToInt32(remote.Revision))
		remoteInput, err := remoteToInput(remote)
		if err != nil {
			return err
		}
		if remoteJSON, err = canonicalJSON(remoteInput); err != nil {
			return err
		}
	}

	changed := remoteJSON != localJSON
	unified := ""
	if changed {
		edits := myers.ComputeEdits(span.URIFromPath(fromLabel), remoteJSON, localJSON)
		unified = fmt.Sprint(gotextdiff.ToUnified(fromLabel, name+" (local)", remoteJSON, edits))
	}

	if app.cli.Output == "json" {
		b, err := json.MarshalIndent(map[string]any{
			"jobDefinitionName": name,
			"changed":           changed,
			"diff":              unified,
		}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(b))
		return nil
	}

	if remote == nil {
		fmt.Fprintf(os.Stderr, "# %s is not registered yet — showing the full definition as new\n", name)
	}
	fmt.Fprint(os.Stdout, unified)
	return nil
}

// latestJobDefinition returns the highest ACTIVE revision for name, or nil.
func (app *App) latestJobDefinition(name string) (*types.JobDefinition, error) {
	revs, err := app.listActiveRevisions(name)
	if err != nil {
		return nil, err
	}
	if len(revs) == 0 {
		return nil, nil
	}
	return &revs[0], nil
}
