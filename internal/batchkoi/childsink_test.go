package batchkoi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"

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

// windowSources emits one "batch[N]" Source per window child once its stream is
// known — this is what the >32 pager feeds resolog for the visible page.
func TestWindowSources(t *testing.T) {
	child := func(idx int32, stream string) types.JobDetail {
		return types.JobDetail{
			ArrayProperties: &types.ArrayPropertiesDetail{Index: aws.Int32(idx)},
			Container:       &types.ContainerDetail{LogStreamName: aws.String(stream)},
		}
	}
	fb := &fakeBatch{jobs: map[string]types.JobDetail{
		"p:33": child(33, "s33"),
		"p:34": child(34, "s34"),
	}}
	app := testApp(t, fb, nil, jobdefImg1)
	app.poll = time.Millisecond

	res := app.windowSources(context.Background(), "p", "/aws/batch/job", 33, 35)
	got := map[string]string{} // label -> stream
	timeout := time.After(2 * time.Second)
	for {
		select {
		case s, ok := <-res.Sources:
			if !ok {
				if got["batch[33]"] != "s33" || got["batch[34]"] != "s34" {
					t.Fatalf("windowSources = %+v, want batch[33]=s33, batch[34]=s34", got)
				}
				return
			}
			if s.LogGroup != "/aws/batch/job" {
				t.Errorf("source %q log group = %q", s.Label, s.LogGroup)
			}
			got[s.Label] = s.LogStream
		case <-timeout:
			t.Fatal("timed out draining windowSources")
		}
	}
}
