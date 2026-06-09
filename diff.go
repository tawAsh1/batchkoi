package main

import (
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
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
	if remote == nil {
		fmt.Fprintf(os.Stderr, "# %s is not registered yet — showing the full definition as new\n", name)
	} else {
		fromLabel = fmt.Sprintf("%s:%d (remote)", name, aws.ToInt32(remote.Revision))
		remoteInput, err := remoteToInput(remote)
		if err != nil {
			return err
		}
		if remoteJSON, err = canonicalJSON(remoteInput); err != nil {
			return err
		}
	}

	if remoteJSON == localJSON {
		return nil // no differences
	}

	edits := myers.ComputeEdits(span.URIFromPath(fromLabel), remoteJSON, localJSON)
	fmt.Fprint(os.Stdout, gotextdiff.ToUnified(fromLabel, name+" (local)", remoteJSON, edits))
	return nil
}

// latestJobDefinition returns the highest ACTIVE revision for name, or nil if
// none are registered.
func (app *App) latestJobDefinition(name string) (*types.JobDefinition, error) {
	var latest *types.JobDefinition
	var token *string
	for {
		out, err := app.batch.DescribeJobDefinitions(app.ctx, &batch.DescribeJobDefinitionsInput{
			JobDefinitionName: aws.String(name),
			Status:            aws.String("ACTIVE"),
			NextToken:         token,
		})
		if err != nil {
			return nil, fmt.Errorf("DescribeJobDefinitions: %w", err)
		}
		for i := range out.JobDefinitions {
			jd := out.JobDefinitions[i]
			if latest == nil || aws.ToInt32(jd.Revision) > aws.ToInt32(latest.Revision) {
				latest = &jd
			}
		}
		if aws.ToString(out.NextToken) == "" {
			break
		}
		token = out.NextToken
	}
	return latest, nil
}
