package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DataDir       string `yaml:"data_dir"`
	SessionDB     string `yaml:"session_db"`
	IndexDB       string `yaml:"index_db"`
	RetentionDays int    `yaml:"retention_days"`
	RotationHour  int    `yaml:"rotation_hour"`
	LogLevel      string `yaml:"log_level"`
	AlertWebhook  string `yaml:"alert_webhook"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := &Config{
		RetentionDays: 90,
		RotationHour:  4,
		LogLevel:      "info",
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data_dir is required")
	}
	if c.SessionDB == "" {
		return fmt.Errorf("session_db is required")
	}
	if c.IndexDB == "" {
		return fmt.Errorf("index_db is required")
	}
	if c.RetentionDays < 0 {
		return fmt.Errorf("retention_days must be >= 0")
	}
	if c.RotationHour < 0 || c.RotationHour > 23 {
		return fmt.Errorf("rotation_hour must be 0..23")
	}
	c.DataDir = filepath.Clean(c.DataDir)
	c.SessionDB = filepath.Clean(c.SessionDB)
	c.IndexDB = filepath.Clean(c.IndexDB)
	return nil
}

// EnsureDirs creates parent directories for data and DB files. Called on
// daemon/pair startup so operators don't have to mkdir manually.
func (c *Config) EnsureDirs() error {
	for _, p := range []string{c.DataDir, filepath.Dir(c.SessionDB), filepath.Dir(c.IndexDB)} {
		if err := os.MkdirAll(p, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", p, err)
		}
	}
	return nil
}
