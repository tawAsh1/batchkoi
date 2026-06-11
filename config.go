package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-envparse"
	goversion "github.com/hashicorp/go-version"
	"sigs.k8s.io/yaml"
)

// Config is the batchkoi configuration file (batchkoi.yml), modeled after
// ecspresso's ecspresso.yml.
type Config struct {
	RequiredVersion string         `json:"required_version,omitempty"`
	Region          string         `json:"region,omitempty"`
	JobDefinition   string         `json:"job_definition"`
	JobQueue        string         `json:"job_queue,omitempty"`
	Plugins         []PluginConfig `json:"plugins,omitempty"`

	dir string
}

// PluginConfig declares an optional plugin (tfstate, ssm, ...) that registers
// extra native functions for the job definition template.
type PluginConfig struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config,omitempty"`
}

// LoadConfig reads and parses batchkoi.yml.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", path, err)
	}
	if c.JobDefinition == "" {
		return nil, fmt.Errorf("config %s: job_definition is required", path)
	}
	if err := checkRequiredVersion(c.RequiredVersion, version); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	c.dir = filepath.Dir(path)
	c.JobDefinition = c.resolve(c.JobDefinition)
	return &c, nil
}

// checkRequiredVersion enforces the config's required_version constraint
// (ecspresso-style, e.g. ">= 0.2.0, < 1"). Dev builds whose version string
// doesn't parse as semver skip the check.
func checkRequiredVersion(constraint, current string) error {
	if constraint == "" {
		return nil
	}
	cons, err := goversion.NewConstraint(constraint)
	if err != nil {
		return fmt.Errorf("invalid required_version %q: %w", constraint, err)
	}
	v, err := goversion.NewVersion(current)
	if err != nil {
		return nil // dev build — can't compare, let it run
	}
	if !cons.Check(v) {
		return fmt.Errorf("batchkoi %s does not satisfy required_version %q", current, constraint)
	}
	return nil
}

// exportEnvFiles parses each file (hashicorp/go-envparse, lambroll-compatible)
// and exports its variables into the process environment, so they are visible
// to env()/must_env() in templates.
func exportEnvFiles(files []string) error {
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("envfile %s: %w", path, err)
		}
		vars, err := envparse.Parse(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("envfile %s: %w", path, err)
		}
		for k, v := range vars {
			if err := os.Setenv(k, v); err != nil {
				return fmt.Errorf("envfile %s: set %s: %w", path, k, err)
			}
		}
	}
	return nil
}

// resolve makes a relative path absolute against the config file's directory.
// Absolute paths and URLs (containing "://") are returned unchanged.
func (c *Config) resolve(p string) string {
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, "://") {
		return p
	}
	return filepath.Join(c.dir, p)
}
