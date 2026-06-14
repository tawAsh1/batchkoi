package batchkoi

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"

	"github.com/tawAsh1/resolog"
	rlpoll "github.com/tawAsh1/resolog/backend/poll"
	rlbatch "github.com/tawAsh1/resolog/resolver/batch"
)

const defaultLogGroup = "/aws/batch/job"

type RunCmd struct {
	Queue    string            `name:"queue" short:"q" help:"Job queue to submit to (overrides job_queue in config)."`
	Revision string            `name:"revision" aliases:"rev" help:"Run an existing revision: 'latest' or a number N. Default: register the local definition only if it changed, then run the latest."`
	Name     string            `name:"name" help:"Job name (default: <jobDefinitionName>-<unixtime>)."`
	Command  []string          `name:"command" help:"Override the container command (repeatable)."`
	Env      map[string]string `name:"env" short:"e" help:"Override/add container environment variables (KEY=VALUE, repeatable)."`
	Array    int32             `name:"array" help:"Submit as an array job with N children (2-10000); child logs are tailed with per-child prefixes."`
	NoWait   bool              `name:"no-wait" help:"Submit and return the job id without tailing logs."`
	DryRun   bool              `name:"dry-run" help:"Show what would be registered and submitted without changing anything."`
}

// RunResult is the outcome of a run.
type RunResult struct {
	JobName            string           `json:"jobName"`
	JobID              string           `json:"jobId"`
	JobDefinition      string           `json:"jobDefinition"`
	Status             string           `json:"status"`
	ExitCode           *int32           `json:"exitCode,omitempty"`
	Reason             string           `json:"reason,omitempty"`
	ArraySize          int32            `json:"arraySize,omitempty"`
	ArrayStatusSummary map[string]int32 `json:"arrayStatusSummary,omitempty"`
}

func (r RunResult) String() string {
	s := fmt.Sprintf("%s (%s): %s", r.JobName, r.JobID, r.Status)
	if r.ArraySize > 0 {
		s += fmt.Sprintf(" (array: %d/%d succeeded)", r.ArrayStatusSummary["SUCCEEDED"], r.ArraySize)
	}
	if r.ExitCode != nil {
		s += fmt.Sprintf(" exit=%d", *r.ExitCode)
	}
	if r.Reason != "" {
		s += " reason=" + r.Reason
	}
	return s
}

func (c *RunCmd) Run(app *App) error {
	if c.Array != 0 && (c.Array < 2 || c.Array > 10000) {
		return fmt.Errorf("--array must be between 2 and 10000 (AWS Batch limits)")
	}
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

	queue := c.Queue
	if queue == "" {
		queue = app.config.JobQueue
	}
	if queue == "" {
		return fmt.Errorf("no job queue: pass --queue or set job_queue in %s", app.cli.Config)
	}

	if c.DryRun {
		return c.dryRun(app, local, name, queue)
	}

	jobDef, err := c.resolveJobDefinition(app, local, name)
	if err != nil {
		return err
	}

	jobName := c.Name
	if jobName == "" {
		jobName = fmt.Sprintf("%s-%d", name, time.Now().Unix())
	}

	in := &batch.SubmitJobInput{
		JobName:       aws.String(jobName),
		JobQueue:      aws.String(queue),
		JobDefinition: aws.String(jobDef),
	}
	if ov := containerOverrides(c.Command, c.Env); ov != nil {
		warnInapplicableOverrides(local)
		in.ContainerOverrides = ov
	}
	if c.Array > 0 {
		in.ArrayProperties = &types.ArrayProperties{Size: aws.Int32(c.Array)}
	}

	out, err := app.batch.SubmitJob(app.ctx, in)
	if err != nil {
		return fmt.Errorf("SubmitJob: %w", err)
	}
	res := RunResult{
		JobName:       jobName,
		JobID:         aws.ToString(out.JobId),
		JobDefinition: jobDef,
		Status:        string(types.JobStatusSubmitted),
		ArraySize:     c.Array,
	}

	if c.NoWait {
		return app.emit(res)
	}

	// Keep stdout clean per mode: text → logs on stdout; json → logs on stderr,
	// final JSON on stdout. Progress lines always go to stderr.
	logW := io.Writer(os.Stdout)
	progressW := io.Writer(os.Stderr)
	if app.cli.Output == "json" {
		logW = os.Stderr
	}
	if c.Array > 0 {
		fmt.Fprintf(progressW, "submitted %s (%s) → queue %s [array:%d]\n", res.JobName, res.JobID, queue, c.Array)
	} else {
		fmt.Fprintf(progressW, "submitted %s (%s) → queue %s\n", res.JobName, res.JobID, queue)
	}

	var final *types.JobDetail
	if c.Array > 0 {
		final, err = app.waitAndTailArray(res.JobID, c.Array, resolveLogGroup(local), logW, progressW)
	} else {
		final, err = app.waitAndTail(res.JobID, logW, progressW)
	}
	if err != nil {
		return err
	}
	res.Status = string(final.Status)
	if final.Container != nil {
		res.ExitCode = final.Container.ExitCode
		res.Reason = aws.ToString(final.Container.Reason)
	}
	if final.ArrayProperties != nil {
		res.ArrayStatusSummary = final.ArrayProperties.StatusSummary
	}
	if res.Reason == "" {
		res.Reason = aws.ToString(final.StatusReason)
	}

	if err := app.emit(res); err != nil {
		return err
	}
	if final.Status == types.JobStatusFailed {
		return fmt.Errorf("job %s FAILED", res.JobName)
	}
	return nil
}

// warnInapplicableOverrides flags --command/--env on definitions where
// SubmitJob's containerOverrides don't apply: Batch ignores or rejects them
// for EKS and multi-node jobs (they only work for ECS/Fargate container jobs).
func warnInapplicableOverrides(local *batch.RegisterJobDefinitionInput) {
	switch {
	case local.EksProperties != nil:
		fmt.Fprintln(os.Stderr, "warning: --command/--env set containerOverrides, which do not apply to EKS job definitions — the overrides will not take effect")
	case local.NodeProperties != nil:
		fmt.Fprintln(os.Stderr, "warning: --command/--env set containerOverrides, which do not apply to multi-node job definitions — use nodeOverrides via the AWS CLI instead")
	}
}

// RunDryRunResult is the preview of a run.
type RunDryRunResult struct {
	JobName       string            `json:"jobName"`
	JobQueue      string            `json:"jobQueue"`
	JobDefinition string            `json:"jobDefinition"`
	WouldRegister bool              `json:"wouldRegister"`
	DryRun        bool              `json:"dryRun"`
	Command       []string          `json:"command,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	ArraySize     int32             `json:"arraySize,omitempty"`
}

func (r RunDryRunResult) String() string {
	var b strings.Builder
	if r.WouldRegister {
		fmt.Fprintf(&b, "would register %s (definition changed)\n", r.JobDefinition)
	}
	fmt.Fprintf(&b, "would submit %s → queue %s (definition %s)", r.JobName, r.JobQueue, r.JobDefinition)
	if r.ArraySize > 0 {
		fmt.Fprintf(&b, " [array:%d]", r.ArraySize)
	}
	b.WriteString("\nDRY RUN — nothing was changed")
	return b.String()
}

// dryRun previews a run: whether a new revision would be registered, and
// which definition / queue / name the job would be submitted with. Mirrors
// resolveJobDefinition without registering or submitting anything.
func (c *RunCmd) dryRun(app *App, local *batch.RegisterJobDefinitionInput, name, queue string) error {
	if containerOverrides(c.Command, c.Env) != nil {
		warnInapplicableOverrides(local)
	}
	res := &RunDryRunResult{
		JobName:   c.Name,
		JobQueue:  queue,
		DryRun:    true,
		Command:   c.Command,
		Env:       c.Env,
		ArraySize: c.Array,
	}
	if res.JobName == "" {
		res.JobName = name + "-<unixtime>"
	}

	switch c.Revision {
	case "":
		latest, err := app.latestJobDefinition(name)
		if err != nil {
			return err
		}
		changed, _, err := computeDiff(local, latest, name)
		if err != nil {
			return err
		}
		if changed {
			// INACTIVE revisions count too: the next revision is max(all)+1.
			all, err := app.listRevisions(name, "")
			if err != nil {
				return err
			}
			res.WouldRegister = true
			res.JobDefinition = fmt.Sprintf("%s:%d", name, maxRevision(all)+1)
		} else {
			res.JobDefinition = aws.ToString(latest.JobDefinitionArn)
		}
	case "latest":
		latest, err := app.latestJobDefinition(name)
		if err != nil {
			return err
		}
		if latest == nil {
			return fmt.Errorf("no active revision found for %s", name)
		}
		res.JobDefinition = fmt.Sprintf("%s:%d", name, aws.ToInt32(latest.Revision))
	default:
		n, err := strconv.Atoi(c.Revision)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid --revision %q (use 'latest' or a positive number)", c.Revision)
		}
		res.JobDefinition = fmt.Sprintf("%s:%d", name, n)
	}
	return app.emit(res)
}

// resolveJobDefinition decides which job definition to submit.
func (c *RunCmd) resolveJobDefinition(app *App, local *batch.RegisterJobDefinitionInput, name string) (string, error) {
	switch c.Revision {
	case "":
		// Smart-register, like deploy: only register when the rendered
		// definition differs from the latest revision.
		reg, latest, err := app.registerIfChanged(local, name)
		if err != nil {
			return "", err
		}
		if reg != nil {
			fmt.Fprintf(os.Stderr, "registered %s:%d\n", reg.JobDefinitionName, reg.Revision)
			return reg.JobDefinitionArn, nil
		}
		fmt.Fprintf(os.Stderr, "no changes — using %s:%d\n", name, aws.ToInt32(latest.Revision))
		return aws.ToString(latest.JobDefinitionArn), nil
	case "latest":
		latest, err := app.latestJobDefinition(name)
		if err != nil {
			return "", err
		}
		if latest == nil {
			return "", fmt.Errorf("no active revision found for %s", name)
		}
		return fmt.Sprintf("%s:%d", name, aws.ToInt32(latest.Revision)), nil
	default:
		n, err := strconv.Atoi(c.Revision)
		if err != nil || n <= 0 {
			return "", fmt.Errorf("invalid --revision %q (use 'latest' or a positive number)", c.Revision)
		}
		return fmt.Sprintf("%s:%d", name, n), nil
	}
}

// containerOverrides builds SubmitJob container overrides from --command and
// --env, or nil when neither is set. Env keys are sorted for stable requests.
func containerOverrides(command []string, env map[string]string) *types.ContainerOverrides {
	if len(command) == 0 && len(env) == 0 {
		return nil
	}
	ov := &types.ContainerOverrides{Command: command}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ov.Environment = append(ov.Environment, types.KeyValuePair{
			Name:  aws.String(k),
			Value: aws.String(env[k]),
		})
	}
	return ov
}

func resolveLogGroup(in *batch.RegisterJobDefinitionInput) string {
	if in.ContainerProperties != nil {
		return logGroupOf(in.ContainerProperties.LogConfiguration)
	}
	return defaultLogGroup
}

// waitAndTail tails a single job's container log via resolog while batchkoi
// keeps polling the job's status for the [STATUS] lines, the final JobDetail
// and the exit-on-FAILED decision. resolog owns tail shutdown (its resolver's
// Done signal + a grace window to catch awslogs' ingestion lag); batchkoi never
// cancels the tail itself — only a Ctrl-C (shared context) does.
func (app *App) waitAndTail(jobID string, logW, progressW io.Writer) (*types.JobDetail, error) {
	res, err := rlbatch.New(app.batch, rlbatch.WithPollInterval(app.pollEvery())).Resolve(app.ctx, jobID)
	if err != nil {
		return nil, err
	}
	backend := rlpoll.New(app.logs, rlpoll.Options{Follow: true, Interval: app.pollEvery()})
	sink := &childSink{out: logW} // single job: label "batch/<name>" → no prefix

	tailDone := make(chan struct{})
	go func() {
		defer close(tailDone)
		_ = resolog.Tail(app.ctx, res, backend, sink,
			resolog.WithGracePeriod(app.pollEvery()*3),
			resolog.WithErrorHandler(func(s resolog.Source, e error) {
				fmt.Fprintf(progressW, "warning: cannot read logs from %s: %v\n", s.LogGroup, e)
			}))
	}()

	final, err := app.waitForTerminal(jobID, progressW)
	<-tailDone // let resolog finish its grace drain before returning
	return final, err
}

// waitForTerminal polls the job until it reaches a terminal state, printing a
// [STATUS] line on each transition and returning the final JobDetail. It does
// not read logs — that is resolog's job.
func (app *App) waitForTerminal(jobID string, progressW io.Writer) (*types.JobDetail, error) {
	var lastStatus types.JobStatus
	for {
		job, err := app.describeJob(jobID)
		if err != nil {
			if app.ctx.Err() != nil {
				return nil, fmt.Errorf("interrupted — job %s may still be running", jobID)
			}
			return nil, err
		}
		if job.Status != lastStatus {
			fmt.Fprintf(progressW, "[%s]\n", job.Status)
			lastStatus = job.Status
		}
		if job.Status == types.JobStatusSucceeded || job.Status == types.JobStatusFailed {
			return job, nil
		}
		if app.sleep(app.pollEvery()) != nil {
			return nil, fmt.Errorf("interrupted — job %s is still running", jobID)
		}
	}
}

// sleep waits for d or until the app context is cancelled (returning its error).
func (app *App) sleep(d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-app.ctx.Done():
		return app.ctx.Err()
	case <-t.C:
		return nil
	}
}

func (app *App) describeJob(jobID string) (*types.JobDetail, error) {
	out, err := app.batch.DescribeJobs(app.ctx, &batch.DescribeJobsInput{Jobs: []string{jobID}})
	if err != nil {
		return nil, fmt.Errorf("DescribeJobs: %w", err)
	}
	if len(out.Jobs) == 0 {
		return nil, fmt.Errorf("job %s not found", jobID)
	}
	return &out.Jobs[0], nil
}

// tailOnce drains every available page of new log events, writing each line
// behind prefix. GetLogEvents signals exhaustion by returning the same forward
// token that was passed in, so a single-call version would print at most one
// page per poll and could truncate the final flush.
func (app *App) tailOnce(logGroup, stream string, token *string, w io.Writer, prefix string) (*string, error) {
	for {
		out, err := app.logs.GetLogEvents(app.ctx, &cloudwatchlogs.GetLogEventsInput{
			LogGroupName:  aws.String(logGroup),
			LogStreamName: aws.String(stream),
			StartFromHead: aws.Bool(true),
			NextToken:     token,
		})
		if err != nil {
			return token, err
		}
		for _, e := range out.Events {
			fmt.Fprintln(w, prefix+aws.ToString(e.Message))
		}
		if aws.ToString(out.NextForwardToken) == aws.ToString(token) {
			return out.NextForwardToken, nil
		}
		token = out.NextForwardToken
	}
}
