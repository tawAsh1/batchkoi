package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderJobDefinitionExtVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobdef.jsonnet")
	src := `{
  jobDefinitionName: 'app-' + std.extVar('stage'),
  type: 'container',
  containerProperties: { vcpus: std.extVar('cpu') },
}`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{
		ctx:    context.Background(),
		cli:    &CLI{ExtStr: map[string]string{"stage": "prod"}, ExtCode: map[string]string{"cpu": "4"}},
		config: &Config{JobDefinition: path},
	}
	in, err := app.loadJobDefinition()
	if err != nil {
		t.Fatal(err)
	}
	if got := *in.JobDefinitionName; got != "app-prod" {
		t.Errorf("jobDefinitionName = %q, want %q", got, "app-prod")
	}
	if got := *in.ContainerProperties.Vcpus; got != 4 {
		t.Errorf("vcpus = %d, want 4", got)
	}
}

func TestRenderJobDefinitionMissingExtVar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobdef.jsonnet")
	if err := os.WriteFile(path, []byte(`{ jobDefinitionName: std.extVar('nope') }`), 0644); err != nil {
		t.Fatal(err)
	}
	app := &App{
		ctx:    context.Background(),
		cli:    &CLI{},
		config: &Config{JobDefinition: path},
	}
	_, err := app.loadJobDefinition()
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("want undefined external variable error, got %v", err)
	}
}
