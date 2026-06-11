package batchkoi

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

func TestContainerOverrides(t *testing.T) {
	if ov := containerOverrides(nil, nil); ov != nil {
		t.Errorf("no overrides: got %+v, want nil", ov)
	}

	ov := containerOverrides([]string{"echo", "hi"}, map[string]string{"B": "2", "A": "1"})
	if len(ov.Command) != 2 || ov.Command[0] != "echo" {
		t.Errorf("command = %v", ov.Command)
	}
	if len(ov.Environment) != 2 {
		t.Fatalf("environment = %v, want 2 entries", ov.Environment)
	}
	// Keys must be sorted for stable SubmitJob requests.
	if *ov.Environment[0].Name != "A" || *ov.Environment[0].Value != "1" ||
		*ov.Environment[1].Name != "B" || *ov.Environment[1].Value != "2" {
		t.Errorf("environment not sorted: %v", ov.Environment)
	}

	if ov := containerOverrides(nil, map[string]string{"A": "1"}); ov == nil || len(ov.Command) != 0 {
		t.Errorf("env-only override: got %+v", ov)
	}
}
