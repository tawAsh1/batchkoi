package main

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

func TestCanonicalJSONPreservesUserDataKeys(t *testing.T) {
	in := &batch.RegisterJobDefinitionInput{
		JobDefinitionName: aws.String("myjob"),
		Type:              types.JobDefinitionTypeContainer,
		Tags: map[string]string{
			"ManagedBy":   "batchkoi",
			"Cost-Center": "ABC",
		},
		Parameters: map[string]string{"InputBucket": "s3://x"},
		ContainerProperties: &types.ContainerProperties{
			Image: aws.String("img"),
			LogConfiguration: &types.LogConfiguration{
				LogDriver: types.LogDriverAwslogs,
				Options:   map[string]string{"awslogs-group": "/g", "Max-Size": "10m"},
			},
		},
	}
	got, err := canonicalJSON(in)
	if err != nil {
		t.Fatal(err)
	}

	// Schema keys → lowerCamelCase.
	for _, want := range []string{`"jobDefinitionName"`, `"containerProperties"`, `"logConfiguration"`, `"tags"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing schema key %s in:\n%s", want, got)
		}
	}
	// User-data map keys must pass through verbatim.
	for _, want := range []string{`"ManagedBy"`, `"Cost-Center"`, `"InputBucket"`, `"Max-Size"`, `"awslogs-group"`} {
		if !strings.Contains(got, want) {
			t.Errorf("user-data key %s was mangled:\n%s", want, got)
		}
	}
	for _, bad := range []string{`"managedBy"`, `"cost-Center"`, `"inputBucket"`, `"max-Size"`} {
		if strings.Contains(got, bad) {
			t.Errorf("user-data key was lowercased to %s:\n%s", bad, got)
		}
	}
}

func TestCanonicalJSONSortsEnvironmentAndSecrets(t *testing.T) {
	def := func(envOrder, secretOrder []string) *batch.RegisterJobDefinitionInput {
		cp := &types.ContainerProperties{Image: aws.String("img")}
		for _, n := range envOrder {
			cp.Environment = append(cp.Environment, types.KeyValuePair{
				Name: aws.String(n), Value: aws.String("v-" + n),
			})
		}
		for _, n := range secretOrder {
			cp.Secrets = append(cp.Secrets, types.Secret{
				Name: aws.String(n), ValueFrom: aws.String("arn-" + n),
			})
		}
		return &batch.RegisterJobDefinitionInput{
			JobDefinitionName:   aws.String("myjob"),
			Type:                types.JobDefinitionTypeContainer,
			ContainerProperties: cp,
		}
	}

	a, err := canonicalJSON(def([]string{"B", "A", "C"}, []string{"S2", "S1"}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalJSON(def([]string{"A", "C", "B"}, []string{"S1", "S2"}))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("pure reordering of environment/secrets produced a diff:\n%s\n---\n%s", a, b)
	}
	if iA, iB := strings.Index(a, `"v-A"`), strings.Index(a, `"v-B"`); iA > iB {
		t.Errorf("environment not sorted by name:\n%s", a)
	}
}

func TestCanonicalJSONRemoteRoundTrip(t *testing.T) {
	// A remote JobDefinition (as DescribeJobDefinitions returns it) must
	// canonicalize identically to the equivalent local input, tags included.
	remote := &types.JobDefinition{
		JobDefinitionName: aws.String("myjob"),
		JobDefinitionArn:  aws.String("arn:aws:batch:xx:1:job-definition/myjob:3"),
		Revision:          aws.Int32(3),
		Status:            aws.String("ACTIVE"),
		Type:              aws.String("container"),
		Tags:              map[string]string{"ManagedBy": "batchkoi"},
		ContainerProperties: &types.ContainerProperties{
			Image: aws.String("img"),
			Environment: []types.KeyValuePair{
				{Name: aws.String("Z"), Value: aws.String("1")},
				{Name: aws.String("A"), Value: aws.String("2")},
			},
		},
	}
	in, err := remoteToInput(remote)
	if err != nil {
		t.Fatal(err)
	}
	got, err := canonicalJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	local := &batch.RegisterJobDefinitionInput{
		JobDefinitionName: aws.String("myjob"),
		Type:              types.JobDefinitionTypeContainer,
		Tags:              map[string]string{"ManagedBy": "batchkoi"},
		ContainerProperties: &types.ContainerProperties{
			Image: aws.String("img"),
			Environment: []types.KeyValuePair{
				{Name: aws.String("A"), Value: aws.String("2")},
				{Name: aws.String("Z"), Value: aws.String("1")},
			},
		},
	}
	want, err := canonicalJSON(local)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("remote round-trip != local:\nremote:\n%s\nlocal:\n%s", got, want)
	}
}
