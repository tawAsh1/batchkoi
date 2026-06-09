package main

import (
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

// listActiveRevisions returns every ACTIVE revision of name, sorted newest-first.
func (app *App) listActiveRevisions(name string) ([]types.JobDefinition, error) {
	var all []types.JobDefinition
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
		all = append(all, out.JobDefinitions...)
		if aws.ToString(out.NextToken) == "" {
			break
		}
		token = out.NextToken
	}
	sort.Slice(all, func(i, j int) bool {
		return aws.ToInt32(all[i].Revision) > aws.ToInt32(all[j].Revision)
	})
	return all, nil
}
