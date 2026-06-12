package batchkoi

import (
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

type LogsCmd struct {
	JobID  string `arg:"" name:"job-id" help:"Job id, or <job-id>:<index> for an array child."`
	Follow bool   `name:"follow" short:"f" help:"Keep tailing until the job reaches a terminal state (like run does)."`
}

// Run prints the CloudWatch logs of an existing job. Without --follow it
// dumps what is there and returns; with --follow it tails until the job
// terminates, exiting non-zero on FAILED like run. An array parent id with
// --follow gets the same rich tail as run --array: every child interleaved
// behind a colored prefix, with a progress bar (and paging beyond 32).
func (c *LogsCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		return err
	}
	job, err := app.describeJob(c.JobID)
	if err != nil {
		return err
	}

	// Same stdout discipline as run: text → logs on stdout; json → logs on
	// stderr, final JSON on stdout.
	logW := app.out()
	progressW := io.Writer(os.Stderr)
	if app.cli.Output == "json" {
		logW = os.Stderr
	}
	group := app.logGroupForJob(job)

	final := job
	switch {
	case job.ArrayProperties != nil && job.ArrayProperties.Index == nil: // array parent
		if !c.Follow {
			return fmt.Errorf("%s is an array parent — pass --follow to tail all children, or use %s:<index> for one child", c.JobID, c.JobID)
		}
		final, err = app.waitAndTailArray(c.JobID, aws.ToInt32(job.ArrayProperties.Size), group, logW, progressW)
		if err != nil {
			return err
		}
	case c.Follow:
		final, err = app.waitAndTail(c.JobID, group, logW, progressW)
		if err != nil {
			return err
		}
	default:
		if job.Container == nil {
			return fmt.Errorf("job %s has no container details (multi-node jobs are not supported)", c.JobID)
		}
		stream := aws.ToString(job.Container.LogStreamName)
		if stream == "" {
			return fmt.Errorf("job %s has no log stream yet (status %s)", c.JobID, job.Status)
		}
		if _, err := app.tailOnce(group, stream, nil, logW, ""); err != nil {
			return fmt.Errorf("cannot read logs from %s/%s: %w", group, stream, err)
		}
	}

	res := RunResult{
		JobName:       aws.ToString(final.JobName),
		JobID:         c.JobID,
		JobDefinition: aws.ToString(final.JobDefinition),
		Status:        string(final.Status),
	}
	if final.Container != nil {
		res.ExitCode = final.Container.ExitCode
		res.Reason = aws.ToString(final.Container.Reason)
	}
	if final.ArrayProperties != nil {
		res.ArraySize = aws.ToInt32(final.ArrayProperties.Size)
		res.ArrayStatusSummary = final.ArrayProperties.StatusSummary
	}
	if res.Reason == "" {
		res.Reason = aws.ToString(final.StatusReason)
	}
	if app.cli.Output == "json" {
		if err := app.emit(res); err != nil {
			return err
		}
	}
	if c.Follow && final.Status == types.JobStatusFailed {
		return fmt.Errorf("job %s FAILED", res.JobName)
	}
	return nil
}

// logGroupForJob resolves the awslogs group for an existing job: from its
// container detail when present, else from its job definition (array parents
// may carry no container log configuration), else the Batch default.
func (app *App) logGroupForJob(job *types.JobDetail) string {
	if job.Container != nil && job.Container.LogConfiguration != nil {
		return logGroupOf(job.Container.LogConfiguration)
	}
	if arn := aws.ToString(job.JobDefinition); arn != "" {
		out, err := app.batch.DescribeJobDefinitions(app.ctx, &batch.DescribeJobDefinitionsInput{
			JobDefinitions: []string{arn},
		})
		if err == nil && len(out.JobDefinitions) > 0 && out.JobDefinitions[0].ContainerProperties != nil {
			return logGroupOf(out.JobDefinitions[0].ContainerProperties.LogConfiguration)
		}
	}
	return defaultLogGroup
}

// logGroupOf resolves the awslogs group from a log configuration, defaulting
// to AWS Batch's /aws/batch/job.
func logGroupOf(lc *types.LogConfiguration) string {
	if lc != nil {
		if g := lc.Options["awslogs-group"]; g != "" {
			return g
		}
	}
	return defaultLogGroup
}
