package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

func TestResolveLogGroup(t *testing.T) {
	def := &batch.RegisterJobDefinitionInput{
		ContainerProperties: &types.ContainerProperties{},
	}
	if got := resolveLogGroup(def); got != defaultLogGroup {
		t.Errorf("default: got %q, want %q", got, defaultLogGroup)
	}

	custom := &batch.RegisterJobDefinitionInput{
		ContainerProperties: &types.ContainerProperties{
			LogConfiguration: &types.LogConfiguration{
				Options: map[string]string{"awslogs-group": "/my/group"},
			},
		},
	}
	if got := resolveLogGroup(custom); got != "/my/group" {
		t.Errorf("custom: got %q, want %q", got, "/my/group")
	}
}
