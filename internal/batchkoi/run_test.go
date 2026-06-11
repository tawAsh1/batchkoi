package batchkoi

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

func TestResolveLogGroup(t *testing.T) {
	def := &batch.RegisterJobDefinitionInput{
		ContainerProperties: &types.ContainerProperties{},
	}
	if got := resolveLogGroup(def); got != defaultLogGroup {
		t.Errorf("default: got %q, want %q", got, defaultLogGroup)
	}

	custom := &batch.RegisterJobDefinitionInput{
		ContainerProperties: &types.ContainerProperties{
			LogConfiguration: &types.LogConfiguration{
				Options: map[string]string{"awslogs-group": "/my/group"},
			},
		},
	}
	if got := resolveLogGroup(custom); got != "/my/group" {
		t.Errorf("custom: got %q, want %q", got, "/my/group")
	}
}

func TestContainerOverrides(t *testing.T) {
	if ov := containerOverrides(nil, nil); ov != nil {
		t.Errorf("no overrides: got %+v, want nil", ov)
	}

	ov := containerOverrides([]string{"echo", "hi"}, map[string]string{"B": "2", "A": "1"})
	if len(ov.Command) != 2 || ov.Command[0] != "echo" {
		t.Errorf("command = %v", ov.Command)
	}
	if len(ov.Environment) != 2 {
		t.Fatalf("environment = %v, want 2 entries", ov.Environment)
	}
	// Keys must be sorted for stable SubmitJob requests.
	if *ov.Environment[0].Name != "A" || *ov.Environment[0].Value != "1" ||
		*ov.Environment[1].Name != "B" || *ov.Environment[1].Value != "2" {
		t.Errorf("environment not sorted: %v", ov.Environment)
	}

	if ov := containerOverrides(nil, map[string]string{"A": "1"}); ov == nil || len(ov.Command) != 0 {
		t.Errorf("env-only override: got %+v", ov)
	}
}

func TestArrayProgress(t *testing.T) {
	cases := []struct {
		name    string
		size    int32
		summary map[string]int32
		want    string
	}{
		{"empty summary", 10, nil, "▱▱▱▱▱▱▱▱▱▱ 0/10 done"},
		{"running", 10, map[string]int32{"RUNNING": 4, "SUCCEEDED": 3}, "▰▰▰▱▱▱▱▱▱▱ 3/10 done, 4 running"},
		{"with failures", 4, map[string]int32{"SUCCEEDED": 2, "FAILED": 2}, "▰▰▰▰▰▰▰▰▰▰ 4/4 done (2 failed)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := arrayProgress(tc.size, tc.summary, false); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChildPrefix(t *testing.T) {
	if got := childPrefix(3, 2, false); got != " 3 | " {
		t.Errorf("plain: got %q, want %q", got, " 3 | ")
	}
	got := childPrefix(0, 1, true)
	if !strings.HasPrefix(got, "\x1b[36m") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("colored: got %q, want cyan-wrapped prefix", got)
	}
	if !strings.Contains(got, "0 | ") {
		t.Errorf("colored: got %q, want it to contain %q", got, "0 | ")
	}
}

func TestColorEnabledNonTerminal(t *testing.T) {
	if colorEnabled(&strings.Builder{}) {
		t.Error("non-file writer must not enable color")
	}
}

func TestChildPagerMove(t *testing.T) {
	p := &childPager{pages: 4}
	p.move(-1)
	if got := p.page.Load(); got != 0 {
		t.Errorf("move below 0: page = %d, want 0", got)
	}
	p.move(1)
	p.move(1)
	if got := p.page.Load(); got != 2 {
		t.Errorf("two moves right: page = %d, want 2", got)
	}
	for i := 0; i < 10; i++ {
		p.move(1)
	}
	if got := p.page.Load(); got != 3 {
		t.Errorf("clamped at last page: page = %d, want 3", got)
	}
}

func TestPageBanner(t *testing.T) {
	want := "── children 32–63 · page 2/4 · ←/→ to switch ──"
	if got := pageBanner(32, 64, 1, 4, false); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCRLFWriter(t *testing.T) {
	var b strings.Builder
	if _, err := (crlfWriter{&b}).Write([]byte("a\nb\n")); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "a\r\nb\r\n" {
		t.Errorf("got %q, want %q", got, "a\r\nb\r\n")
	}
}
