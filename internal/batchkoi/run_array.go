package batchkoi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"golang.org/x/term"
)

// maxTailedChildren caps how many child log streams are tailed at once.
// GetLogEvents has a 25 TPS default account quota; 32 children polled every
// 2s stay safely under it. Larger arrays are tailed one page of 32 at a time
// (arrow keys switch pages), or show the progress bar only off-terminal.
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
// state, interleaving child log streams behind a colored per-child prefix
// (docker-compose style) and printing a progress bar as children finish.
// Arrays larger than maxTailedChildren are tailed one page at a time.
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

	tailLogs := true
	var pager *childPager
	if size > maxTailedChildren {
		if pager = startChildPager(size, maxTailedChildren, logW); pager != nil {
			defer pager.restore()
			// Raw mode disables output post-processing, so rewrite \n.
			logW = crlfWriter{logW}
			progressW = crlfWriter{progressW}
			fmt.Fprintf(progressW, "array of %d children — tailing %d at a time, ←/→ (or p/n) to switch pages\n",
				size, maxTailedChildren)
		} else {
			tailLogs = false
			fmt.Fprintf(progressW, "array of %d children — log tailing is capped at %d, showing progress only\n",
				size, maxTailedChildren)
		}
	}

	var lastStatus types.JobStatus
	lastProgress := ""
	lastPage := int32(-1)
	warned := false
	tailPage := func() {
		page, offset := children, 0
		if pager != nil {
			cur := pager.page.Load()
			offset = int(cur) * maxTailedChildren
			end := min(offset+maxTailedChildren, len(children))
			page = children[offset:end]
			if cur != lastPage {
				fmt.Fprintln(progressW, pageBanner(offset, end, int(cur), int(pager.pages), color))
				lastPage = cur
			}
		}
		app.refreshChildStreams(page, offset)
		for _, ch := range page {
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
			if line := arrayProgress(size, parent.ArrayProperties.StatusSummary, color); line != lastProgress {
				fmt.Fprintln(progressW, line)
				lastProgress = line
			}
		}
		if tailLogs {
			tailPage()
		}
		if parent.Status == types.JobStatusSucceeded || parent.Status == types.JobStatusFailed {
			// Same grace drain as the single-job tail: awslogs delivers
			// with a few seconds of lag.
			for i := 0; tailLogs && i < 3; i++ {
				if app.sleep(2*time.Second) != nil {
					break
				}
				tailPage()
			}
			return parent, nil
		}
		if app.sleep(2*time.Second) != nil {
			return nil, fmt.Errorf("interrupted — job %s is still running", parentID)
		}
	}
}

// childPager flips which window of children is being tailed, driven by arrow
// keys (or p/n) read from a raw-mode stdin.
type childPager struct {
	pages   int32
	page    atomic.Int32
	restore func()
}

// startChildPager puts stdin into raw mode and listens for paging keys.
// Returns nil (no paging) when stdin or the log writer is not a terminal.
func startChildPager(size, perPage int32, logW io.Writer) *childPager {
	f, ok := logW.(*os.File)
	if !ok || !isCharDevice(f) || !isCharDevice(os.Stdin) {
		return nil
	}
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil
	}
	p := &childPager{pages: (size + perPage - 1) / perPage}
	p.restore = func() { term.Restore(int(os.Stdin.Fd()), old) }
	go p.readKeys()
	return p
}

// readKeys translates keypresses into page moves. Raw mode turns off ISIG,
// so Ctrl-C is re-raised as a real SIGINT to take the normal interrupt path.
func (p *childPager) readKeys() {
	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		switch {
		case n == 1 && buf[0] == 0x03: // Ctrl-C
			if proc, err := os.FindProcess(os.Getpid()); err == nil {
				proc.Signal(os.Interrupt)
			}
		case n == 3 && buf[0] == 0x1b && (buf[1] == '[' || buf[1] == 'O') && buf[2] == 'C': // →
			p.move(1)
		case n == 3 && buf[0] == 0x1b && (buf[1] == '[' || buf[1] == 'O') && buf[2] == 'D': // ←
			p.move(-1)
		case n == 1 && (buf[0] == 'n' || buf[0] == ' '):
			p.move(1)
		case n == 1 && buf[0] == 'p':
			p.move(-1)
		}
	}
}

// move shifts the current page by d, clamped to [0, pages).
func (p *childPager) move(d int32) {
	for {
		cur := p.page.Load()
		next := min(max(cur+d, 0), p.pages-1)
		if next == cur || p.page.CompareAndSwap(cur, next) {
			return
		}
	}
}

// pageBanner is the separator printed when the visible page changes,
// e.g. "── children 32–63 · page 2/4 · ←/→ to switch ──".
func pageBanner(start, end, page, pages int, color bool) string {
	s := fmt.Sprintf("── children %d–%d · page %d/%d · ←/→ to switch ──", start, end-1, page+1, pages)
	if color {
		return "\x1b[2m" + s + "\x1b[0m"
	}
	return s
}

// crlfWriter rewrites \n to \r\n: raw mode disables the terminal's output
// post-processing, so bare newlines would stair-step.
type crlfWriter struct{ w io.Writer }

func (c crlfWriter) Write(b []byte) (int, error) {
	if _, err := c.w.Write(bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))); err != nil {
		return 0, err
	}
	return len(b), nil
}

// refreshChildStreams looks up the log stream of children that don't have one
// yet. children may be a page slice; offset is the array index of children[0].
// Children are matched by their array index — DescribeJobs is best-effort
// here, the next poll retries anything missing or failed.
func (app *App) refreshChildStreams(children []*childTail, offset int) {
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
			if pos := int(aws.ToInt32(job.ArrayProperties.Index)) - offset; pos >= 0 && pos < len(children) {
				children[pos].stream = aws.ToString(job.Container.LogStreamName)
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
	return ok && isCharDevice(f)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
