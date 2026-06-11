package batchkoi

import (
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

type LogsCmd struct {
	JobID  string `arg:"" name:"job-id" help:"Job id, or <job-id>:<index> for an array child."`
	Follow bool   `name:"follow" short:"f" help:"Keep tailing until the job reaches a terminal state (like run does)."`
}

// Run prints the CloudWatch logs of an existing job. Without --follow it
// dumps what is there and returns; with --follow it tails until the job
// terminates, exiting non-zero on FAILED like run.
func (c *LogsCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		return err
	}
	job, err := app.describeJob(c.JobID)
	if err != nil {
		return err
	}
	if job.ArrayProperties != nil && job.ArrayProperties.Index == nil {
		return fmt.Errorf("%s is an array parent and has no log stream — use %s:<index> for a child", c.JobID, c.JobID)
	}
	if job.Container == nil {
		return fmt.Errorf("job %s has no container details (multi-node jobs are not supported)", c.JobID)
	}

	// Same stdout discipline as run: text → logs on stdout; json → logs on
	// stderr, final JSON on stdout.
	logW := io.Writer(os.Stdout)
	progressW := io.Writer(os.Stderr)
	if app.cli.Output == "json" {
		logW = os.Stderr
	}
	group := logGroupOf(job.Container.LogConfiguration)

	final := job
	if c.Follow {
		if final, err = app.waitAndTail(c.JobID, group, logW, progressW); err != nil {
			return err
		}
	} else {
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
