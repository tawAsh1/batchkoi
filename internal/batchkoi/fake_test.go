package batchkoi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// fakeBatch is an in-memory batchAPI: a slice of job definitions plus call
// recording, mimicking the filter semantics batchkoi relies on.
type fakeBatch struct {
	defs         []types.JobDefinition
	jobs         map[string]types.JobDetail
	queues       []types.JobQueueDetail
	registered   []*batch.RegisterJobDefinitionInput
	deregistered []string // ARNs
	submitted    []*batch.SubmitJobInput
}

func (f *fakeBatch) DescribeJobDefinitions(_ context.Context, in *batch.DescribeJobDefinitionsInput, _ ...func(*batch.Options)) (*batch.DescribeJobDefinitionsOutput, error) {
	var out []types.JobDefinition
	for _, jd := range f.defs {
		switch {
		case len(in.JobDefinitions) > 0: // exact specs: ARN or name:rev
			spec := fmt.Sprintf("%s:%d", aws.ToString(jd.JobDefinitionName), aws.ToInt32(jd.Revision))
			for _, want := range in.JobDefinitions {
				if want == spec || want == aws.ToString(jd.JobDefinitionArn) {
					out = append(out, jd)
				}
			}
		default:
			if n := aws.ToString(in.JobDefinitionName); n != "" && n != aws.ToString(jd.JobDefinitionName) {
				continue
			}
			if s := aws.ToString(in.Status); s != "" && s != aws.ToString(jd.Status) {
				continue
			}
			out = append(out, jd)
		}
	}
	return &batch.DescribeJobDefinitionsOutput{JobDefinitions: out}, nil
}

func (f *fakeBatch) RegisterJobDefinition(_ context.Context, in *batch.RegisterJobDefinitionInput, _ ...func(*batch.Options)) (*batch.RegisterJobDefinitionOutput, error) {
	f.registered = append(f.registered, in)
	rev := maxRevision(f.defs) + 1
	name := aws.ToString(in.JobDefinitionName)
	arn := fakeArn(name, rev)
	f.defs = append(f.defs, types.JobDefinition{
		JobDefinitionName:   in.JobDefinitionName,
		Revision:            aws.Int32(rev),
		Status:              aws.String("ACTIVE"),
		JobDefinitionArn:    aws.String(arn),
		Type:                aws.String(string(in.Type)),
		ContainerProperties: in.ContainerProperties,
	})
	return &batch.RegisterJobDefinitionOutput{
		JobDefinitionName: in.JobDefinitionName,
		Revision:          aws.Int32(rev),
		JobDefinitionArn:  aws.String(arn),
	}, nil
}

func (f *fakeBatch) DeregisterJobDefinition(_ context.Context, in *batch.DeregisterJobDefinitionInput, _ ...func(*batch.Options)) (*batch.DeregisterJobDefinitionOutput, error) {
	f.deregistered = append(f.deregistered, aws.ToString(in.JobDefinition))
	for i := range f.defs {
		if aws.ToString(f.defs[i].JobDefinitionArn) == aws.ToString(in.JobDefinition) {
			f.defs[i].Status = aws.String("INACTIVE")
		}
	}
	return &batch.DeregisterJobDefinitionOutput{}, nil
}

func (f *fakeBatch) SubmitJob(_ context.Context, in *batch.SubmitJobInput, _ ...func(*batch.Options)) (*batch.SubmitJobOutput, error) {
	f.submitted = append(f.submitted, in)
	return &batch.SubmitJobOutput{JobId: aws.String("job-1"), JobName: in.JobName}, nil
}

func (f *fakeBatch) DescribeJobs(_ context.Context, in *batch.DescribeJobsInput, _ ...func(*batch.Options)) (*batch.DescribeJobsOutput, error) {
	out := &batch.DescribeJobsOutput{}
	for _, id := range in.Jobs {
		if j, ok := f.jobs[id]; ok {
			out.Jobs = append(out.Jobs, j)
		}
	}
	return out, nil
}

func (f *fakeBatch) DescribeJobQueues(_ context.Context, in *batch.DescribeJobQueuesInput, _ ...func(*batch.Options)) (*batch.DescribeJobQueuesOutput, error) {
	out := &batch.DescribeJobQueuesOutput{}
	for _, q := range f.queues {
		for _, want := range in.JobQueues {
			if want == aws.ToString(q.JobQueueName) {
				out.JobQueues = append(out.JobQueues, q)
			}
		}
	}
	return out, nil
}

// fakeLogs serves one canned page of events per stream, then reports
// exhaustion by echoing the forward token back (the GetLogEvents contract).
type fakeLogs struct {
	events map[string][]string // stream -> messages
	groups []string
	calls  int
}

func (f *fakeLogs) GetLogEvents(_ context.Context, in *cloudwatchlogs.GetLogEventsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.GetLogEventsOutput, error) {
	f.calls++
	stream := aws.ToString(in.LogStreamName)
	msgs, ok := f.events[stream]
	if !ok {
		return nil, &cwltypes.ResourceNotFoundException{Message: aws.String("stream not found")}
	}
	done := aws.String("token-done")
	if aws.ToString(in.NextToken) == "token-done" { // exhausted
		return &cloudwatchlogs.GetLogEventsOutput{NextForwardToken: done}, nil
	}
	out := &cloudwatchlogs.GetLogEventsOutput{NextForwardToken: done}
	for _, m := range msgs {
		out.Events = append(out.Events, cwltypes.OutputLogEvent{Message: aws.String(m)})
	}
	return out, nil
}

func (f *fakeLogs) DescribeLogGroups(_ context.Context, in *cloudwatchlogs.DescribeLogGroupsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	out := &cloudwatchlogs.DescribeLogGroupsOutput{}
	for _, g := range f.groups {
		if strings.HasPrefix(g, aws.ToString(in.LogGroupNamePrefix)) {
			out.LogGroups = append(out.LogGroups, cwltypes.LogGroup{LogGroupName: aws.String(g)})
		}
	}
	return out, nil
}

func fakeArn(name string, rev int32) string {
	return "arn:aws:batch:ap-northeast-1:123456789012:job-definition/" + name + ":" + strconv.Itoa(int(rev))
}

// activeDef builds an ACTIVE revision with the given image ("" for none),
// matching the shape DescribeJobDefinitions returns.
func activeDef(name string, rev int32, image string) types.JobDefinition {
	jd := types.JobDefinition{
		JobDefinitionName: aws.String(name),
		Revision:          aws.Int32(rev),
		Status:            aws.String("ACTIVE"),
		JobDefinitionArn:  aws.String(fakeArn(name, rev)),
		Type:              aws.String("container"),
	}
	if image != "" {
		jd.ContainerProperties = &types.ContainerProperties{Image: aws.String(image)}
	}
	return jd
}

func inactive(jd types.JobDefinition) types.JobDefinition {
	jd.Status = aws.String("INACTIVE")
	return jd
}

// testApp wires an App around fakes and a jobdef file written to a temp dir,
// bypassing setup()'s AWS wiring (setup early-returns when config is set).
func testApp(t *testing.T, fb *fakeBatch, fl *fakeLogs, jobdefJSON string) *App {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "jobdef.json")
	if err := os.WriteFile(path, []byte(jobdefJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if fb.jobs == nil {
		fb.jobs = map[string]types.JobDetail{}
	}
	if fl == nil {
		fl = &fakeLogs{}
	}
	return &App{
		ctx:    context.Background(),
		cli:    &CLI{Output: "text"},
		config: &Config{JobDefinition: path, dir: dir},
		batch:  fb,
		logs:   fl,
	}
}
