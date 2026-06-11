package batchkoi

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// maxTailedChildren caps how many child log streams are tailed. GetLogEvents
// has a 25 TPS default account quota; 32 children polled every 2s stay safely
// under it. Larger arrays still run — they just show the progress bar only.
const maxTailedChildren = 32

// childTail is the tail state of one array child: its job id
// ("<parent>:<index>"), display prefix, log stream, and forward token.
type childTail struct {
	id     string
	prefix string
	stream string
	token  *string
}

// waitAndTailArray polls an array job's parent until it reaches a terminal
// state, interleaving every child's log stream behind a colored per-child
// prefix (docker-compose style) and printing a progress bar as children
// finish.
func (app *App) waitAndTailArray(parentID string, size int32, logGroup string, logW, progressW io.Writer) (*types.JobDetail, error) {
	width := len(fmt.Sprintf("%d", size-1))
	color := colorEnabled(logW)
	children := make([]*childTail, size)
	for i := range children {
		children[i] = &childTail{
			id:     fmt.Sprintf("%s:%d", parentID, i),
			prefix: childPrefix(i, width, color),
		}
	}
	tailLogs := size <= maxTailedChildren
	if !tailLogs {
		fmt.Fprintf(progressW, "array of %d children — log tailing is capped at %d, showing progress only\n",
			size, maxTailedChildren)
	}

	var lastStatus types.JobStatus
	lastProgress := ""
	warned := false
	tailAll := func() {
		for _, ch := range children {
			if ch.stream == "" {
				continue
			}
			tok, err := app.tailOnce(logGroup, ch.stream, ch.token, logW, ch.prefix)
			if err == nil {
				ch.token = tok
				continue
			}
			// Same semantics as the single-job tail: a missing stream is
			// normal until the container logs its first event; anything
			// else is warned once instead of failing silently.
			var nf *cwltypes.ResourceNotFoundException
			if !errors.As(err, &nf) && !warned {
				fmt.Fprintf(progressW, "warning: cannot read logs from %s/%s: %v\n", logGroup, ch.stream, err)
				warned = true
			}
		}
	}

	for {
		parent, err := app.describeJob(parentID)
		if err != nil {
			if app.ctx.Err() != nil {
				return nil, fmt.Errorf("interrupted — job %s may still be running", parentID)
			}
			return nil, err
		}
		if parent.Status != lastStatus {
			fmt.Fprintf(progressW, "[%s]\n", parent.Status)
			lastStatus = parent.Status
		}
		if parent.ArrayProperties != nil {
			if line := arrayProgress(size, parent.ArrayProperties.StatusSummary, colorEnabled(progressW)); line != lastProgress {
				fmt.Fprintln(progressW, line)
				lastProgress = line
			}
		}
		if tailLogs {
			app.refreshChildStreams(children)
			tailAll()
		}
		if parent.Status == types.JobStatusSucceeded || parent.Status == types.JobStatusFailed {
			// Same grace drain as the single-job tail: awslogs delivers
			// with a few seconds of lag.
			for i := 0; tailLogs && i < 3; i++ {
				if app.sleep(2*time.Second) != nil {
					break
				}
				tailAll()
			}
			return parent, nil
		}
		if app.sleep(2*time.Second) != nil {
			return nil, fmt.Errorf("interrupted — job %s is still running", parentID)
		}
	}
}

// refreshChildStreams looks up the log stream of children that don't have one
// yet. Children are matched by their array index — DescribeJobs is best-effort
// here, the next poll retries anything missing or failed.
func (app *App) refreshChildStreams(children []*childTail) {
	var missing []string
	for _, ch := range children {
		if ch.stream == "" {
			missing = append(missing, ch.id)
		}
	}
	for start := 0; start < len(missing); start += 100 { // DescribeJobs caps at 100 ids
		out, err := app.batch.DescribeJobs(app.ctx, &batch.DescribeJobsInput{
			Jobs: missing[start:min(start+100, len(missing))],
		})
		if err != nil {
			return
		}
		for _, job := range out.Jobs {
			if job.ArrayProperties == nil || job.ArrayProperties.Index == nil || job.Container == nil {
				continue
			}
			if idx := int(aws.ToInt32(job.ArrayProperties.Index)); idx >= 0 && idx < len(children) {
				children[idx].stream = aws.ToString(job.Container.LogStreamName)
			}
		}
	}
}

// arrayProgress renders a one-line progress bar from the parent's
// statusSummary, e.g. "▰▰▰▱▱▱▱▱▱▱ 3/10 done, 4 running (1 failed)".
func arrayProgress(size int32, summary map[string]int32, color bool) string {
	succeeded, failed := summary["SUCCEEDED"], summary["FAILED"]
	done := succeeded + failed
	const barWidth = 10
	filled := 0
	if size > 0 {
		filled = int(int64(done) * barWidth / int64(size))
	}
	bar := strings.Repeat("▰", filled) + strings.Repeat("▱", barWidth-filled)
	if color {
		bar = "\x1b[32m" + strings.Repeat("▰", filled) + "\x1b[0m" + strings.Repeat("▱", barWidth-filled)
	}
	line := fmt.Sprintf("%s %d/%d done", bar, done, size)
	if running := summary["RUNNING"]; running > 0 {
		line += fmt.Sprintf(", %d running", running)
	}
	if failed > 0 {
		suffix := fmt.Sprintf(" (%d failed)", failed)
		if color {
			suffix = fmt.Sprintf(" (\x1b[31m%d failed\x1b[0m)", failed)
		}
		line += suffix
	}
	return line
}

// childColors cycles per child index (docker-compose style):
// cyan, yellow, green, magenta, blue, then their bright variants.
var childColors = []int{36, 33, 32, 35, 34, 96, 93, 92, 95, 94}

// childPrefix renders the per-child log prefix, e.g. " 3 | ", colored by
// index and padded to the widest index so the pipes line up.
func childPrefix(i, width int, color bool) string {
	label := fmt.Sprintf("%*d | ", width, i)
	if !color {
		return label
	}
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", childColors[i%len(childColors)], label)
}

// colorEnabled reports whether w is a terminal and color is not disabled via
// the NO_COLOR convention or TERM=dumb.
func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
