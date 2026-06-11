package main

import (
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

// listRevisions returns every revision of name with the given status
// ("ACTIVE", "INACTIVE", or "" for both), sorted newest-first.
func (app *App) listRevisions(name, status string) ([]types.JobDefinition, error) {
	in := &batch.DescribeJobDefinitionsInput{
		JobDefinitionName: aws.String(name),
	}
	if status != "" {
		in.Status = aws.String(status)
	}
	var all []types.JobDefinition
	for {
		out, err := app.batch.DescribeJobDefinitions(app.ctx, in)
		if err != nil {
			return nil, fmt.Errorf("DescribeJobDefinitions: %w", err)
		}
		all = append(all, out.JobDefinitions...)
		if aws.ToString(out.NextToken) == "" {
			break
		}
		in.NextToken = out.NextToken
	}
	sort.Slice(all, func(i, j int) bool {
		return aws.ToInt32(all[i].Revision) > aws.ToInt32(all[j].Revision)
	})
	return all, nil
}

// listActiveRevisions returns every ACTIVE revision of name, sorted newest-first.
func (app *App) listActiveRevisions(name string) ([]types.JobDefinition, error) {
	return app.listRevisions(name, "ACTIVE")
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

// maxRevision returns the highest revision number among revs (0 when empty).
// Batch never reuses revision numbers, so the next register always creates
// maxRevision(all revisions, INACTIVE included) + 1.
func maxRevision(revs []types.JobDefinition) int32 {
	var max int32
	for _, jd := range revs {
		if rev := aws.ToInt32(jd.Revision); rev > max {
			max = rev
		}
	}
	return max
}

// jobDefinitionByRevision fetches one specific revision (ACTIVE or INACTIVE).
func (app *App) jobDefinitionByRevision(name string, rev int) (*types.JobDefinition, error) {
	spec := fmt.Sprintf("%s:%d", name, rev)
	out, err := app.batch.DescribeJobDefinitions(app.ctx, &batch.DescribeJobDefinitionsInput{
		JobDefinitions: []string{spec},
	})
	if err != nil {
		return nil, fmt.Errorf("DescribeJobDefinitions %s: %w", spec, err)
	}
	if len(out.JobDefinitions) == 0 {
		return nil, fmt.Errorf("job definition %s not found", spec)
	}
	return &out.JobDefinitions[0], nil
}
