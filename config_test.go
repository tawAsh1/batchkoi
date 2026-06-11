package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRequiredVersion(t *testing.T) {
	cases := []struct {
		name       string
		constraint string
		current    string
		wantErr    bool
	}{
		{"no constraint", "", "0.1.0", false},
		{"satisfied", ">= 0.1.0", "0.2.0", false},
		{"not satisfied", ">= 1.0.0", "0.2.0", true},
		{"range satisfied", ">= 0.1.0, < 1", "v0.5.3", false},
		{"range not satisfied", ">= 0.1.0, < 0.5", "0.5.0", true},
		{"dev build skips", ">= 1.0.0", "dev", false},
		{"invalid constraint", "not-a-version", "0.1.0", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkRequiredVersion(tc.constraint, tc.current)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkRequiredVersion(%q, %q) error = %v, wantErr %v",
					tc.constraint, tc.current, err, tc.wantErr)
			}
		})
	}
}

func TestExportEnvFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "BK_TEST_FOO=foo\nexport BK_TEST_BAR=\"bar baz\"\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BK_TEST_FOO", "")
	t.Setenv("BK_TEST_BAR", "")

	if err := exportEnvFiles([]string{path}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("BK_TEST_FOO"); got != "foo" {
		t.Errorf("BK_TEST_FOO = %q, want %q", got, "foo")
	}
	if got := os.Getenv("BK_TEST_BAR"); got != "bar baz" {
		t.Errorf("BK_TEST_BAR = %q, want %q", got, "bar baz")
	}

	if err := exportEnvFiles([]string{filepath.Join(dir, "missing.env")}); err == nil {
		t.Error("want error for missing envfile, got nil")
	}
}

func TestVerifySecretNonARN(t *testing.T) {
	app := &App{}
	if c := app.verifySecret("secret FOO", "plain-name"); c.Status != checkSkip {
		t.Errorf("non-ARN: status = %s, want SKIP", c.Status)
	}
	if c := app.verifySecret("secret FOO", ""); c.Status != checkNG {
		t.Errorf("empty: status = %s, want NG", c.Status)
	}
	if c := app.verifySecret("secret FOO", "arn:aws:s3:::bucket/key"); c.Status != checkSkip ||
		!strings.Contains(c.Detail, "s3") {
		t.Errorf("unsupported service: got %+v, want SKIP mentioning s3", c)
	}
}
