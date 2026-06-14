package batchkoi

import (
	"context"
	"strings"
	"testing"

	"github.com/tawAsh1/resolog"
)

func TestChildSink(t *testing.T) {
	var buf strings.Builder
	s := &childSink{out: &buf, width: 2, color: false}

	ch := make(chan resolog.Event, 3)
	ch <- resolog.Event{Source: resolog.Source{Label: "batch[3]"}, Message: "hi"}     // array child → prefix
	ch <- resolog.Event{Source: resolog.Source{Label: "batch/single"}, Message: "yo"} // single job → no prefix
	ch <- resolog.Event{Source: resolog.Source{Label: "batch[10]"}, Message: "wide"}  // width alignment
	close(ch)

	if err := s.Consume(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	want := " 3 | hi\nyo\n10 | wide\n"
	if got := buf.String(); got != want {
		t.Errorf("childSink output = %q, want %q", got, want)
	}
}
