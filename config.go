package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

// Config is the batchkoi configuration file (batchkoi.yml), modeled after
// ecspresso's ecspresso.yml.
type Config struct {
	Region        string         `json:"region,omitempty"`
	JobDefinition string         `json:"job_definition"`
	JobQueue      string         `json:"job_queue,omitempty"`
	Plugins       []PluginConfig `json:"plugins,omitempty"`

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
	c.dir = filepath.Dir(path)
	c.JobDefinition = c.resolve(c.JobDefinition)
	return &c, nil
}

// resolve makes a relative path absolute against the config file's directory.
// Absolute paths and URLs (containing "://") are returned unchanged.
func (c *Config) resolve(p string) string {
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, "://") {
		return p
	}
	return filepath.Join(c.dir, p)
}
