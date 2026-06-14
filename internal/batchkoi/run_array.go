package batchkoi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	"golang.org/x/term"

	"github.com/tawAsh1/resolog"
	rlpoll "github.com/tawAsh1/resolog/backend/poll"
)

// maxTailedChildren caps how many child log streams are tailed at once.
// FilterLogEvents has a per-account TPS quota; one window of 32 children polled
// every couple of seconds stays safely under it. Larger arrays are tailed one
// page of 32 at a time (arrow keys switch pages), or show the progress bar only
// off-terminal.
const maxTailedChildren = 32

// childTail is the discovery state of one array child: its job id
// ("<parent>:<index>") and, once known, its log stream.
type childTail struct {
	id     string
	stream string
}

// waitAndTailArray tails an array job's children behind colored per-child
// prefixes (docker-compose style) with a progress bar, all through resolog.
// Arrays within the tail cap stream every child at once; larger arrays page
// through one window at a time.
func (app *App) waitAndTailArray(parentID string, size int32, logGroup string, logW, progressW io.Writer) (*types.JobDetail, error) {
	if size <= maxTailedChildren {
		return app.tailArrayViaResolog(parentID, size, logW, progressW)
	}
	return app.tailArrayPaged(parentID, size, logGroup, logW, progressW)
}

// tailArrayPaged tails an array larger than maxTailedChildren: a pager shows one
// window of children at a time (arrow keys switch pages), each window tailed via
// resolog. Switching pages cancels the current window's tail and starts the
// next. Keeping the window bounded means the per-child fan-in stays under the
// FilterLogEvents TPS quota without batching. Off-terminal (piped/CI) it falls
// back to the progress bar only, since there is no way to page.
func (app *App) tailArrayPaged(parentID string, size int32, logGroup string, logW, progressW io.Writer) (*types.JobDetail, error) {
	color := colorEnabled(logW)
	width := len(fmt.Sprintf("%d", size-1))

	pager := startChildPager(size, maxTailedChildren, logW)
	if pager == nil {
		fmt.Fprintf(progressW, "array of %d children — log tailing needs a terminal for paging, showing progress only\n", size)
		return app.waitForArrayTerminal(parentID, size, color, progressW)
	}
	defer pager.restore()
	// Raw mode disables output post-processing, so rewrite \n → \r\n.
	logW = crlfWriter{logW}
	progressW = crlfWriter{progressW}
	fmt.Fprintf(progressW, "array of %d children — tailing %d at a time, ←/→ (or p/n) to switch pages\n",
		size, maxTailedChildren)

	var winCancel context.CancelFunc
	var winDone chan struct{}
	stopWindow := func() {
		if winCancel != nil {
			winCancel()
			<-winDone
			winCancel, winDone = nil, nil
		}
	}
	startWindow := func(page int32) {
		offset := int(page) * maxTailedChildren
		end := min(offset+maxTailedChildren, int(size))
		fmt.Fprintln(progressW, pageBanner(offset, end, int(page), int(pager.pages), color))
		wctx, cancel := context.WithCancel(app.ctx)
		done := make(chan struct{})
		winCancel, winDone = cancel, done
		go func() {
			defer close(done)
			res := app.windowSources(wctx, parentID, logGroup, offset, end)
			backend := rlpoll.New(app.logs, rlpoll.Options{Follow: true, Interval: app.pollEvery()})
			sink := &childSink{out: logW, width: width, color: color}
			_ = resolog.Tail(wctx, res, backend, sink,
				resolog.WithErrorHandler(func(s resolog.Source, e error) {
					fmt.Fprintf(progressW, "warning: cannot read logs from %s: %v\n", s.LogGroup, e)
				}))
		}()
	}
	defer stopWindow()

	cur := pager.page.Load()
	startWindow(cur)

	var lastStatus types.JobStatus
	lastProgress := ""
	for {
		if p := pager.page.Load(); p != cur {
			stopWindow()
			cur = p
			startWindow(cur)
		}
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
		if parent.Status == types.JobStatusSucceeded || parent.Status == types.JobStatusFailed {
			// Let the current window catch awslogs' lagged final lines before
			// the deferred stopWindow cancels it.
			app.sleep(app.pollEvery() * 3)
			return parent, nil
		}
		if app.sleep(app.pollEvery()) != nil {
			return nil, fmt.Errorf("interrupted — job %s is still running", parentID)
		}
	}
}

// windowSources is a resolog.Resolution that emits one Source per child in the
// window [offset,end) as each child's log stream appears (labelled "batch[N]"
// so childSink can colour it by index). Done is nil; the caller stops it by
// cancelling ctx — on a page switch or after the terminal grace.
func (app *App) windowSources(ctx context.Context, parentID, logGroup string, offset, end int) resolog.Resolution {
	sources := make(chan resolog.Source)
	go func() {
		defer close(sources)
		children := make([]*childTail, end-offset)
		for i := range children {
			children[i] = &childTail{id: fmt.Sprintf("%s:%d", parentID, offset+i)}
		}
		emitted := make([]bool, len(children))
		for {
			app.refreshChildStreams(children, offset)
			remaining := 0
			for i, ch := range children {
				if ch.stream == "" {
					remaining++
					continue
				}
				if emitted[i] {
					continue
				}
				emitted[i] = true
				select {
				case sources <- resolog.Source{
					Key:       "batch:" + ch.stream,
					Label:     fmt.Sprintf("batch[%d]", offset+i),
					LogGroup:  logGroup,
					LogStream: ch.stream,
				}:
				case <-ctx.Done():
					return
				}
			}
			if remaining == 0 {
				return // every child discovered; discovery complete
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(app.pollEvery()):
			}
		}
	}()
	return resolog.Resolution{Sources: sources, Done: nil}
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
			// Restore the terminal before re-raising: the first SIGINT takes
			// the graceful path (the deferred restore would run anyway), but
			// after it main() resets the disposition to default, so a second
			// Ctrl-C kills the process outright — without this the shell
			// would be left in raw mode. term.Restore is idempotent.
			p.restore()
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
