package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type RegisterCmd struct{}

func (c *RegisterCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		return err
	}
	res, err := app.register()
	if err != nil {
		return err
	}
	return app.emit(res)
}

// RegisterResult is the outcome of registering a new revision.
type RegisterResult struct {
	JobDefinitionName string `json:"jobDefinitionName"`
	Revision          int32  `json:"revision"`
	JobDefinitionArn  string `json:"jobDefinitionArn"`
	Status            string `json:"status"`
}

func (r RegisterResult) String() string {
	return fmt.Sprintf("registered %s:%d\n%s", r.JobDefinitionName, r.Revision, r.JobDefinitionArn)
}

// register renders the local job definition and registers a new revision.
func (app *App) register() (*RegisterResult, error) {
	in, err := app.loadJobDefinition()
	if err != nil {
		return nil, err
	}
	out, err := app.batch.RegisterJobDefinition(app.ctx, in)
	if err != nil {
		return nil, fmt.Errorf("RegisterJobDefinition: %w", err)
	}
	return &RegisterResult{
		JobDefinitionName: aws.ToString(out.JobDefinitionName),
		Revision:          aws.ToInt32(out.Revision),
		JobDefinitionArn:  aws.ToString(out.JobDefinitionArn),
		Status:            "registered",
	}, nil
}
