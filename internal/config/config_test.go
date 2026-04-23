package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadHappyPath(t *testing.T) {
	path := writeConfig(t, `
data_dir: /tmp/ss/data
session_db: /tmp/ss/session.db
index_db: /tmp/ss/index.db
retention_days: 30
rotation_hour: 3
log_level: debug
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/tmp/ss/data" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d", cfg.RetentionDays)
	}
	if cfg.RotationHour != 3 {
		t.Errorf("RotationHour = %d", cfg.RotationHour)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	// Minimal config: only required fields. Defaults should fill in the rest.
	path := writeConfig(t, `
data_dir: /tmp/ss/data
session_db: /tmp/ss/session.db
index_db: /tmp/ss/index.db
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetentionDays != 90 {
		t.Errorf("default retention_days = %d, want 90", cfg.RetentionDays)
	}
	if cfg.RotationHour != 4 {
		t.Errorf("default rotation_hour = %d, want 4", cfg.RotationHour)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default log_level = %q, want info", cfg.LogLevel)
	}
}

func TestLoadMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing data_dir",
			body: "session_db: /s\nindex_db: /i\n",
			want: "data_dir",
		},
		{
			name: "missing session_db",
			body: "data_dir: /d\nindex_db: /i\n",
			want: "session_db",
		},
		{
			name: "missing index_db",
			body: "data_dir: /d\nsession_db: /s\n",
			want: "index_db",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeConfig(t, c.body)
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, should mention %q", err, c.want)
			}
		})
	}
}

func TestLoadInvalidRetentionAndHour(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "negative retention",
			body: "data_dir: /d\nsession_db: /s\nindex_db: /i\nretention_days: -1\n",
			want: "retention_days",
		},
		{
			name: "hour too low",
			body: "data_dir: /d\nsession_db: /s\nindex_db: /i\nrotation_hour: -1\n",
			want: "rotation_hour",
		},
		{
			name: "hour too high",
			body: "data_dir: /d\nsession_db: /s\nindex_db: /i\nrotation_hour: 24\n",
			want: "rotation_hour",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeConfig(t, c.body)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want error mentioning %q, got %v", c.want, err)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	path := writeConfig(t, "::: not valid yaml :::\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("want parse error, got %v", err)
	}
}

func TestEnsureDirsCreatesParents(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{
		DataDir:   filepath.Join(tmp, "data"),
		SessionDB: filepath.Join(tmp, "state", "session.db"),
		IndexDB:   filepath.Join(tmp, "state", "index.db"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{cfg.DataDir, filepath.Dir(cfg.SessionDB), filepath.Dir(cfg.IndexDB)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("dir %q not created: %v", p, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a dir", p)
		}
	}
}
