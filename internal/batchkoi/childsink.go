package batchkoi

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/tawAsh1/resolog"
)

// childSink implements resolog.Sink, rendering each event with batchkoi's own
// per-child colored prefix instead of resolog's default renderer. It maps a
// resolog Source.Label of the form "batch[N]" (the batch resolver's array-child
// labelling) back to child index N and reuses childPrefix, so the output is
// byte-for-byte identical to the home-grown tailer. A label that isn't
// "batch[N]" (e.g. a single job's "batch/<name>") gets no prefix, matching the
// unprefixed single-stream tail.
type childSink struct {
	out   io.Writer
	width int
	color bool
}

// Consume implements resolog.Sink.
func (s *childSink) Consume(ctx context.Context, events <-chan resolog.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e, ok := <-events:
			if !ok {
				return nil
			}
			prefix := ""
			if i := indexFromLabel(e.Source.Label); i >= 0 {
				prefix = childPrefix(i, s.width, s.color)
			}
			fmt.Fprintln(s.out, prefix+e.Message)
		}
	}
}

// indexFromLabel extracts N from a "batch[N]" label, or returns -1 otherwise.
func indexFromLabel(label string) int {
	if !strings.HasPrefix(label, "batch[") || !strings.HasSuffix(label, "]") {
		return -1
	}
	n, err := strconv.Atoi(label[len("batch[") : len(label)-1])
	if err != nil {
		return -1
	}
	return n
}
