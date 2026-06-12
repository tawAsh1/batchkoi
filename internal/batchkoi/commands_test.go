package batchkoi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

const jobdefImg1 = `{"jobDefinitionName":"app","type":"container","containerProperties":{"image":"img:1"}}`
const jobdefImg2 = `{"jobDefinitionName":"app","type":"container","containerProperties":{"image":"img:2"}}`

func TestDeployNoChange(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{activeDef("app", 1, "img:1")}}
	app := testApp(t, fb, nil, jobdefImg1)
	if err := (&DeployCmd{}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.registered) != 0 {
		t.Errorf("registered %d definitions, want 0", len(fb.registered))
	}
}

func TestDeployRegistersWhenChanged(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{activeDef("app", 1, "img:1")}}
	app := testApp(t, fb, nil, jobdefImg2)
	if err := (&DeployCmd{}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.registered) != 1 {
		t.Fatalf("registered %d definitions, want 1", len(fb.registered))
	}
	if got := aws.ToString(fb.registered[0].ContainerProperties.Image); got != "img:2" {
		t.Errorf("registered image = %q, want img:2", got)
	}
}

func TestDeployKeepCountPrunesAfterRegister(t *testing.T) {
	// rev1 is old; local differs, so deploy registers rev2 and --keep-count 1
	// must then prune rev1 (counting the freshly registered revision).
	fb := &fakeBatch{defs: []types.JobDefinition{activeDef("app", 1, "img:1")}}
	app := testApp(t, fb, nil, jobdefImg2)
	if err := (&DeployCmd{KeepCount: 1}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.registered) != 1 {
		t.Fatalf("registered %d definitions, want 1", len(fb.registered))
	}
	want := []string{fakeArn("app", 1)}
	if len(fb.deregistered) != 1 || fb.deregistered[0] != want[0] {
		t.Errorf("deregistered = %v, want %v", fb.deregistered, want)
	}
}

func TestDeployDryRunChangesNothing(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{
		activeDef("app", 2, "img:1"),
		activeDef("app", 1, "img:0"),
	}}
	app := testApp(t, fb, nil, jobdefImg2)
	if err := (&DeployCmd{KeepCount: 1, DryRun: true}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.registered) != 0 || len(fb.deregistered) != 0 {
		t.Errorf("dry-run mutated state: registered=%d deregistered=%v", len(fb.registered), fb.deregistered)
	}
}

func TestRollback(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{
		activeDef("app", 2, "img:2"),
		activeDef("app", 1, "img:1"),
	}}
	app := testApp(t, fb, nil, jobdefImg2)
	if err := (&RollbackCmd{}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.deregistered) != 1 || fb.deregistered[0] != fakeArn("app", 2) {
		t.Errorf("deregistered = %v, want [%s]", fb.deregistered, fakeArn("app", 2))
	}
}

func TestRollbackDryRun(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{
		activeDef("app", 2, "img:2"),
		activeDef("app", 1, "img:1"),
	}}
	app := testApp(t, fb, nil, jobdefImg2)
	if err := (&RollbackCmd{DryRun: true}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.deregistered) != 0 {
		t.Errorf("dry-run deregistered %v, want none", fb.deregistered)
	}
}

func TestRollbackNeedsTwoActive(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{activeDef("app", 1, "img:1")}}
	app := testApp(t, fb, nil, jobdefImg1)
	if err := (&RollbackCmd{}).Run(app); err == nil || !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("want 'at least 2' error, got %v", err)
	}
}

func TestDeregisterCmd(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{
		activeDef("app", 3, "img:3"),
		activeDef("app", 2, "img:2"),
		activeDef("app", 1, "img:1"),
	}}
	app := testApp(t, fb, nil, jobdefImg1)
	if err := (&DeregisterCmd{KeepCount: 2}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.deregistered) != 1 || fb.deregistered[0] != fakeArn("app", 1) {
		t.Errorf("deregistered = %v, want [%s]", fb.deregistered, fakeArn("app", 1))
	}
}

func TestResolveJobDefinition(t *testing.T) {
	newApp := func(local string) (*fakeBatch, *App) {
		fb := &fakeBatch{defs: []types.JobDefinition{
			activeDef("app", 2, "img:2"),
			inactive(activeDef("app", 1, "img:1")),
		}}
		return fb, testApp(t, fb, nil, local)
	}

	t.Run("latest", func(t *testing.T) {
		_, app := newApp(jobdefImg2)
		local, _ := app.loadJobDefinition()
		got, err := (&RunCmd{Revision: "latest"}).resolveJobDefinition(app, local, "app")
		if err != nil || got != "app:2" {
			t.Errorf("got %q, %v; want app:2", got, err)
		}
	})
	t.Run("pinned number", func(t *testing.T) {
		_, app := newApp(jobdefImg2)
		local, _ := app.loadJobDefinition()
		got, err := (&RunCmd{Revision: "1"}).resolveJobDefinition(app, local, "app")
		if err != nil || got != "app:1" {
			t.Errorf("got %q, %v; want app:1", got, err)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		_, app := newApp(jobdefImg2)
		local, _ := app.loadJobDefinition()
		if _, err := (&RunCmd{Revision: "abc"}).resolveJobDefinition(app, local, "app"); err == nil {
			t.Error("want error for invalid revision")
		}
	})
	t.Run("smart register unchanged", func(t *testing.T) {
		fb, app := newApp(jobdefImg2) // local == latest rev2
		local, _ := app.loadJobDefinition()
		got, err := (&RunCmd{}).resolveJobDefinition(app, local, "app")
		if err != nil || got != fakeArn("app", 2) {
			t.Errorf("got %q, %v; want %s", got, err, fakeArn("app", 2))
		}
		if len(fb.registered) != 0 {
			t.Errorf("registered %d, want 0", len(fb.registered))
		}
	})
	t.Run("smart register changed", func(t *testing.T) {
		fb, app := newApp(`{"jobDefinitionName":"app","type":"container","containerProperties":{"image":"img:3"}}`)
		local, _ := app.loadJobDefinition()
		got, err := (&RunCmd{}).resolveJobDefinition(app, local, "app")
		if err != nil || got != fakeArn("app", 3) {
			t.Errorf("got %q, %v; want %s", got, err, fakeArn("app", 3))
		}
		if len(fb.registered) != 1 {
			t.Errorf("registered %d, want 1", len(fb.registered))
		}
	})
}

func TestSummarizeJobDefinitions(t *testing.T) {
	revs := []types.JobDefinition{ // newest-first, as listRevisions returns
		activeDef("beta", 5, "beta:5"),
		activeDef("alpha", 3, "alpha:3"),
		inactive(activeDef("alpha", 2, "alpha:2")),
		activeDef("alpha", 1, "alpha:1"),
	}
	got := summarizeJobDefinitions(revs)
	want := []JobDefinitionSummary{
		{Name: "alpha", Revisions: 3, LatestRevision: 3, Status: "ACTIVE", Image: "alpha:3"},
		{Name: "beta", Revisions: 1, LatestRevision: 5, Status: "ACTIVE", Image: "beta:5"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d summaries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("summary[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestVerifyJobQueue(t *testing.T) {
	fb := &fakeBatch{queues: []types.JobQueueDetail{
		{JobQueueName: aws.String("good"), State: types.JQStateEnabled, Status: types.JQStatusValid},
		{JobQueueName: aws.String("disabled"), State: types.JQStateDisabled, Status: types.JQStatusValid},
	}}
	app := testApp(t, fb, nil, jobdefImg1)
	cases := []struct {
		queue, want string
	}{
		{"good", checkOK},
		{"disabled", checkNG},
		{"missing", checkNG},
		{"", checkSkip},
	}
	for _, tc := range cases {
		if c := app.verifyJobQueue(tc.queue); c.Status != tc.want {
			t.Errorf("verifyJobQueue(%q) = %s (%s), want %s", tc.queue, c.Status, c.Detail, tc.want)
		}
	}
}

func TestVerifyQueueFlagOverridesConfig(t *testing.T) {
	fb := &fakeBatch{queues: []types.JobQueueDetail{
		{JobQueueName: aws.String("flag-q"), State: types.JQStateEnabled, Status: types.JQStatusValid},
	}}
	// Minimal jobdef without containerProperties: only the queue is checked.
	app := testApp(t, fb, nil, `{"jobDefinitionName":"app","type":"container"}`)
	app.config.JobQueue = "config-q" // doesn't exist → would NG

	if err := (&VerifyCmd{Queue: "flag-q"}).Run(app); err != nil {
		t.Errorf("verify with --queue flag-q failed: %v", err)
	}
	if err := (&VerifyCmd{}).Run(app); err == nil {
		t.Error("verify with config-q should fail (queue missing), got nil")
	}
}

func TestFindJobDefinition(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{
		activeDef("app", 2, "img:2"),
		inactive(activeDef("app", 1, "img:1")),
	}}
	app := testApp(t, fb, nil, jobdefImg2)

	cases := []struct {
		spec    string
		wantRev int32
		wantErr bool
	}{
		{"app", 2, false},             // bare name → latest ACTIVE
		{"app:1", 1, false},           // pinned, INACTIVE ok
		{fakeArn("app", 2), 2, false}, // ARN
		{"app:x", 0, true},            // bad revision
		{"ghost", 0, true},            // unknown name
	}
	for _, tc := range cases {
		jd, err := app.findJobDefinition(tc.spec)
		if tc.wantErr {
			if err == nil {
				t.Errorf("findJobDefinition(%q): want error, got %+v", tc.spec, jd)
			}
			continue
		}
		if err != nil {
			t.Errorf("findJobDefinition(%q): %v", tc.spec, err)
			continue
		}
		if got := aws.ToInt32(jd.Revision); got != tc.wantRev {
			t.Errorf("findJobDefinition(%q) revision = %d, want %d", tc.spec, got, tc.wantRev)
		}
	}
}

func TestLogsCmd(t *testing.T) {
	fb := &fakeBatch{jobs: map[string]types.JobDetail{
		"j1": {
			JobId:   aws.String("j1"),
			JobName: aws.String("app-1"),
			Status:  types.JobStatusSucceeded,
			Container: &types.ContainerDetail{
				LogStreamName: aws.String("app/default/abc"),
				ExitCode:      aws.Int32(0),
			},
		},
		"parent": {
			JobId:           aws.String("parent"),
			JobName:         aws.String("app-arr"),
			Status:          types.JobStatusRunning,
			ArrayProperties: &types.ArrayPropertiesDetail{Size: aws.Int32(5)},
		},
		"pending": {
			JobId:     aws.String("pending"),
			JobName:   aws.String("app-2"),
			Status:    types.JobStatusRunnable,
			Container: &types.ContainerDetail{},
		},
	}}
	fl := &fakeLogs{events: map[string][]string{"app/default/abc": {"hello", "world"}}}
	app := testApp(t, fb, fl, jobdefImg1)

	if err := (&LogsCmd{JobID: "j1"}).Run(app); err != nil {
		t.Errorf("logs j1: %v", err)
	}
	if fl.calls == 0 {
		t.Error("logs j1 never called GetLogEvents")
	}
	if err := (&LogsCmd{JobID: "parent"}).Run(app); err == nil || !strings.Contains(err.Error(), "array parent") {
		t.Errorf("logs parent: want array parent error, got %v", err)
	}
	if err := (&LogsCmd{JobID: "pending"}).Run(app); err == nil || !strings.Contains(err.Error(), "no log stream") {
		t.Errorf("logs pending: want no-log-stream error, got %v", err)
	}
	if err := (&LogsCmd{JobID: "ghost"}).Run(app); err == nil {
		t.Error("logs ghost: want error for unknown job")
	}
}

// jsonOut runs cmd with -o json and returns the parsed top-level object.
func jsonOut(t *testing.T, app *App, run func() error) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	app.cli.Output = "json"
	app.stdout = &buf
	if err := run(); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	return m
}

// assertEmptyArray fails unless m[key] is present and is [] — not null and
// not missing. Empty collections must stay iterable for jq consumers.
func assertEmptyArray(t *testing.T, m map[string]any, key string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("%s: key missing, want []", key)
		return
	}
	arr, ok := v.([]any)
	if !ok {
		t.Errorf("%s = %v (%T), want []", key, v, v)
		return
	}
	if len(arr) != 0 {
		t.Errorf("%s = %v, want empty", key, arr)
	}
}

func TestDeployJSONEmptyCollections(t *testing.T) {
	// No --keep-count and no changes: deregistered/kept must still be [].
	fb := &fakeBatch{defs: []types.JobDefinition{activeDef("app", 1, "img:1")}}
	app := testApp(t, fb, nil, jobdefImg1)
	m := jsonOut(t, app, func() error { return (&DeployCmd{}).Run(app) })
	assertEmptyArray(t, m, "deregistered")
	assertEmptyArray(t, m, "kept")
}

func TestRevisionsJSONEmpty(t *testing.T) {
	// Nothing registered yet: revisions must be [], not null.
	fb := &fakeBatch{}
	app := testApp(t, fb, nil, jobdefImg1)
	m := jsonOut(t, app, func() error { return (&RevisionsCmd{}).Run(app) })
	assertEmptyArray(t, m, "revisions")
}

func TestListJSONEmpty(t *testing.T) {
	fb := &fakeBatch{}
	app := testApp(t, fb, nil, jobdefImg1)
	m := jsonOut(t, app, func() error { return (&ListCmd{}).Run(app) })
	assertEmptyArray(t, m, "jobDefinitions")
}

func TestRegisterIfChangedRegistersTheDiffedInput(t *testing.T) {
	// register() must submit the exact input that was diffed, not a re-render.
	fb := &fakeBatch{defs: []types.JobDefinition{activeDef("app", 1, "img:1")}}
	app := testApp(t, fb, nil, jobdefImg2)
	local, err := app.loadJobDefinition()
	if err != nil {
		t.Fatal(err)
	}
	reg, _, err := app.registerIfChanged(local, "app")
	if err != nil || reg == nil {
		t.Fatalf("registerIfChanged = %+v, %v; want a registration", reg, err)
	}
	if len(fb.registered) != 1 || fb.registered[0] != local {
		t.Error("registered a re-rendered definition, not the one that was diffed")
	}
}

func TestRegisterCmd(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{activeDef("app", 1, "img:1")}}
	app := testApp(t, fb, nil, jobdefImg1) // register is unconditional, same image is fine
	if err := (&RegisterCmd{}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.registered) != 1 {
		t.Fatalf("registered %d definitions, want 1", len(fb.registered))
	}
}

func TestRegisterDryRunPredictsNextRevision(t *testing.T) {
	// rev2 is ACTIVE, rev1 INACTIVE: the prediction must count both → next is 3.
	fb := &fakeBatch{defs: []types.JobDefinition{
		activeDef("app", 2, "img:2"),
		inactive(activeDef("app", 1, "img:1")),
	}}
	app := testApp(t, fb, nil, jobdefImg2)
	m := jsonOut(t, app, func() error { return (&RegisterCmd{DryRun: true}).Run(app) })
	if got := m["nextRevision"]; got != float64(3) {
		t.Errorf("nextRevision = %v, want 3", got)
	}
	if len(fb.registered) != 0 {
		t.Errorf("dry-run registered %d definitions, want 0", len(fb.registered))
	}
}

func TestRunDryRun(t *testing.T) {
	newApp := func(local string) (*fakeBatch, *App) {
		fb := &fakeBatch{defs: []types.JobDefinition{
			activeDef("app", 2, "img:2"),
			inactive(activeDef("app", 1, "img:1")),
		}}
		return fb, testApp(t, fb, nil, local)
	}
	assertNothingChanged := func(t *testing.T, fb *fakeBatch) {
		t.Helper()
		if len(fb.registered) != 0 || len(fb.submitted) != 0 {
			t.Errorf("dry-run mutated state: registered=%d submitted=%d", len(fb.registered), len(fb.submitted))
		}
	}

	t.Run("unchanged uses latest", func(t *testing.T) {
		fb, app := newApp(jobdefImg2)
		m := jsonOut(t, app, func() error { return (&RunCmd{DryRun: true, Queue: "q"}).Run(app) })
		if m["wouldRegister"] != false || m["jobDefinition"] != fakeArn("app", 2) {
			t.Errorf("got wouldRegister=%v jobDefinition=%v, want false / %s", m["wouldRegister"], m["jobDefinition"], fakeArn("app", 2))
		}
		assertNothingChanged(t, fb)
	})
	t.Run("changed would register next revision", func(t *testing.T) {
		fb, app := newApp(`{"jobDefinitionName":"app","type":"container","containerProperties":{"image":"img:3"}}`)
		m := jsonOut(t, app, func() error { return (&RunCmd{DryRun: true, Queue: "q"}).Run(app) })
		if m["wouldRegister"] != true || m["jobDefinition"] != "app:3" {
			t.Errorf("got wouldRegister=%v jobDefinition=%v, want true / app:3", m["wouldRegister"], m["jobDefinition"])
		}
		assertNothingChanged(t, fb)
	})
	t.Run("pinned revision", func(t *testing.T) {
		fb, app := newApp(jobdefImg2)
		m := jsonOut(t, app, func() error { return (&RunCmd{DryRun: true, Queue: "q", Revision: "1"}).Run(app) })
		if m["wouldRegister"] != false || m["jobDefinition"] != "app:1" {
			t.Errorf("got wouldRegister=%v jobDefinition=%v, want false / app:1", m["wouldRegister"], m["jobDefinition"])
		}
		assertNothingChanged(t, fb)
	})
}

func TestRenderCmd(t *testing.T) {
	fb := &fakeBatch{}
	app := testApp(t, fb, nil, jobdefImg1)
	var buf bytes.Buffer
	app.stdout = &buf
	if err := (&RenderCmd{}).Run(app); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("render output is not JSON: %v\n%s", err, buf.String())
	}
	if m["jobDefinitionName"] != "app" {
		t.Errorf("jobDefinitionName = %v, want app", m["jobDefinitionName"])
	}

	bad := testApp(t, fb, nil, `{not json`)
	bad.stdout = &bytes.Buffer{}
	if err := (&RenderCmd{}).Run(bad); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("want 'not valid JSON' error, got %v", err)
	}
}

func TestInitCmdThenDiffShowsNoChanges(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{activeDef("app", 2, "img:2")}}
	app := testApp(t, fb, nil, jobdefImg1) // the jobdef file is unused by init
	dir := t.TempDir()
	app.cli.Config = filepath.Join(dir, "batchkoi.yml")

	if err := (&InitCmd{JobDefinition: "app"}).Run(app); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"batchkoi.yml", "jobdef.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("init did not write %s: %v", f, err)
		}
	}
	if err := (&InitCmd{JobDefinition: "app"}).Run(app); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("re-init without --force: want 'already exists' error, got %v", err)
	}

	// The init doc promises `diff` right after init shows no changes.
	app2 := &App{
		ctx:    context.Background(),
		cli:    &CLI{Config: app.cli.Config, Output: "json"},
		batch:  fb,
		logs:   &fakeLogs{},
		stdout: &bytes.Buffer{},
	}
	m := jsonOut(t, app2, func() error { return (&DiffCmd{}).Run(app2) })
	if m["changed"] != false {
		t.Errorf("diff right after init: changed = %v, want false\ndiff: %v", m["changed"], m["diff"])
	}
}

func TestListRevisionsPaginated(t *testing.T) {
	fb := &fakeBatch{pageSize: 2, defs: []types.JobDefinition{
		activeDef("app", 1, "img:1"),
		inactive(activeDef("app", 2, "img:2")),
		activeDef("app", 3, "img:3"),
		activeDef("app", 4, "img:4"),
		activeDef("app", 5, "img:5"),
	}}
	app := testApp(t, fb, nil, jobdefImg1)

	revs, err := app.listRevisions("app", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 5 {
		t.Fatalf("got %d revisions across pages, want 5", len(revs))
	}
	if got := aws.ToInt32(revs[0].Revision); got != 5 {
		t.Errorf("revisions not sorted newest-first: revs[0] = %d, want 5", got)
	}

	actives, err := app.listActiveRevisions("app")
	if err != nil {
		t.Fatal(err)
	}
	if len(actives) != 4 {
		t.Errorf("got %d ACTIVE revisions, want 4", len(actives))
	}
}

func TestComputeDiffIgnoresFargateDefault(t *testing.T) {
	// Registering a FARGATE definition without fargatePlatformConfiguration
	// makes AWS add {platformVersion: LATEST}; that alone must not read as a
	// change, or every deploy would register a new revision.
	local := &batch.RegisterJobDefinitionInput{
		JobDefinitionName: aws.String("app"),
		Type:              types.JobDefinitionTypeContainer,
		ContainerProperties: &types.ContainerProperties{
			Image: aws.String("img:1"),
		},
	}
	remote := activeDef("app", 1, "img:1")
	remote.ContainerProperties.FargatePlatformConfiguration = &types.FargatePlatformConfiguration{
		PlatformVersion: aws.String("LATEST"),
	}
	changed, _, err := computeDiff(local, &remote, "app")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("server-filled platformVersion LATEST reported as a change")
	}

	// A pinned platform version is a real difference and must still show.
	remote.ContainerProperties.FargatePlatformConfiguration.PlatformVersion = aws.String("1.4.0")
	changed, _, err = computeDiff(local, &remote, "app")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("pinned platformVersion 1.4.0 not reported as a change")
	}

	// And a local explicit value still compares normally.
	local.ContainerProperties.FargatePlatformConfiguration = &types.FargatePlatformConfiguration{
		PlatformVersion: aws.String("1.4.0"),
	}
	changed, _, err = computeDiff(local, &remote, "app")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("identical explicit platformVersion reported as a change")
	}
}

func TestLogsArrayFollow(t *testing.T) {
	fb := &fakeBatch{jobs: map[string]types.JobDetail{
		"parent": {
			JobId:         aws.String("parent"),
			JobName:       aws.String("app-arr"),
			JobDefinition: aws.String(fakeArn("app", 1)),
			Status:        types.JobStatusSucceeded,
			ArrayProperties: &types.ArrayPropertiesDetail{
				Size:          aws.Int32(2),
				StatusSummary: map[string]int32{"SUCCEEDED": 2},
			},
		},
		"parent:0": {
			JobId:           aws.String("parent:0"),
			Status:          types.JobStatusSucceeded,
			ArrayProperties: &types.ArrayPropertiesDetail{Index: aws.Int32(0)},
			Container:       &types.ContainerDetail{LogStreamName: aws.String("app/default/c0")},
		},
		"parent:1": {
			JobId:           aws.String("parent:1"),
			Status:          types.JobStatusSucceeded,
			ArrayProperties: &types.ArrayPropertiesDetail{Index: aws.Int32(1)},
			Container:       &types.ContainerDetail{LogStreamName: aws.String("app/default/c1")},
		},
	}}
	fl := &fakeLogs{events: map[string][]string{
		"app/default/c0": {"hello-from-0"},
		"app/default/c1": {"hello-from-1"},
	}}
	app := testApp(t, fb, fl, jobdefImg1)
	app.poll = time.Millisecond
	var buf bytes.Buffer
	app.stdout = &buf

	// Parent without --follow points at the rich tail instead of failing flat.
	if err := (&LogsCmd{JobID: "parent"}).Run(app); err == nil || !strings.Contains(err.Error(), "--follow") {
		t.Errorf("logs parent without --follow: want error suggesting --follow, got %v", err)
	}

	if err := (&LogsCmd{JobID: "parent", Follow: true}).Run(app); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "0 | hello-from-0") || !strings.Contains(out, "1 | hello-from-1") {
		t.Errorf("missing per-child prefixed logs, got:\n%s", out)
	}
}

func TestRegisterCopyOfRevision(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{
		activeDef("app", 2, "img:2"),
		inactive(activeDef("app", 1, "img:1")),
	}}
	app := testApp(t, fb, nil, jobdefImg2)
	if err := (&RegisterCmd{Revision: 1}).Run(app); err != nil {
		t.Fatal(err)
	}
	if len(fb.registered) != 1 {
		t.Fatalf("registered %d definitions, want 1", len(fb.registered))
	}
	if got := aws.ToString(fb.registered[0].ContainerProperties.Image); got != "img:1" {
		t.Errorf("registered image = %q, want img:1 (the rev 1 copy)", got)
	}
	if got := maxRevision(fb.defs); got != 3 {
		t.Errorf("new revision = %d, want 3", got)
	}

	if err := (&RegisterCmd{Revision: 99}).Run(app); err == nil {
		t.Error("want error for unknown revision")
	}
}

func TestRegisterCopyDryRun(t *testing.T) {
	fb := &fakeBatch{defs: []types.JobDefinition{
		activeDef("app", 2, "img:2"),
		inactive(activeDef("app", 1, "img:1")),
	}}
	app := testApp(t, fb, nil, jobdefImg2)
	m := jsonOut(t, app, func() error { return (&RegisterCmd{Revision: 1, DryRun: true}).Run(app) })
	if len(fb.registered) != 0 {
		t.Errorf("dry-run registered %d definitions, want 0", len(fb.registered))
	}
	if got := m["nextRevision"].(float64); got != 3 {
		t.Errorf("nextRevision = %v, want 3", got)
	}
	body, _ := json.Marshal(m["jobDefinition"])
	if !strings.Contains(string(body), "img:1") {
		t.Errorf("dry-run body should be the rev 1 copy, got %s", body)
	}
}
