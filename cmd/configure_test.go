package cmd

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

func mockPickDir(path string) func() (string, bool, error) {
	return func() (string, bool, error) {
		return path, false, nil
	}
}

func mockPickDirCancelled() func() (string, bool, error) {
	return func() (string, bool, error) {
		return "", true, nil
	}
}

func mockPickDirSequence(paths ...string) func() (string, bool, error) {
	i := 0
	return func() (string, bool, error) {
		if i >= len(paths) {
			return "", true, nil
		}
		path := paths[i]
		i++
		return path, false, nil
	}
}

func realFSDeps() *deps.MockFileSystem {
	return &deps.MockFileSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			return os.MkdirAll(path, perm)
		},
		WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
			return os.WriteFile(path, data, perm)
		},
	}
}

func TestRunConfigure_FreshConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "pop", "config.toml")

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = oldCfgFile }()

	// "n" to decline adding another directory
	var output bytes.Buffer
	d := &configureDeps{
		FS:      realFSDeps(),
		Stdin:   strings.NewReader("n\n"),
		Stdout:  &output,
		PickDir: mockPickDir("/fake/projects"),
	}

	err := runConfigureWith(d)
	if err != nil {
		t.Fatalf("runConfigureWith() error = %v", err)
	}

	out := output.String()
	if !strings.Contains(out, "/fake/projects/*") {
		t.Errorf("expected pattern in output, got: %s", out)
	}
	if !strings.Contains(out, "Config written to") {
		t.Errorf("expected config written message, got: %s", out)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var cfg config.Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if len(cfg.Projects) != 1 || cfg.Projects[0].Path != "/fake/projects/*" {
		t.Errorf("expected [{/fake/projects/*}], got %v", cfg.Projects)
	}
}

func TestRunConfigure_ExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	existingCfg := config.Config{Projects: []config.ProjectEntry{{Path: "~/existing/pattern"}}}
	data, _ := toml.Marshal(existingCfg)
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("failed to write existing config: %v", err)
	}

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = oldCfgFile }()

	// "y" to add, then "n" to stop
	var output bytes.Buffer
	d := &configureDeps{
		FS:      realFSDeps(),
		Stdin:   strings.NewReader("y\nn\n"),
		Stdout:  &output,
		PickDir: mockPickDir("/new/projects"),
	}

	err := runConfigureWith(d)
	if err != nil {
		t.Fatalf("runConfigureWith() error = %v", err)
	}

	out := output.String()
	if !strings.Contains(out, "Config found at") {
		t.Errorf("expected existing config message, got: %s", out)
	}
	if !strings.Contains(out, "~/existing/pattern") {
		t.Errorf("expected existing pattern in output, got: %s", out)
	}

	written, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var cfg config.Config
	if err := toml.Unmarshal(written, &cfg); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if len(cfg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(cfg.Projects), cfg.Projects)
	}
	if cfg.Projects[0].Path != "~/existing/pattern" {
		t.Errorf("expected first pattern ~/existing/pattern, got %s", cfg.Projects[0].Path)
	}
	if cfg.Projects[1].Path != "/new/projects/*" {
		t.Errorf("expected second pattern /new/projects/*, got %s", cfg.Projects[1].Path)
	}
}

func TestRunConfigure_ExistingConfigDecline(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	existingCfg := config.Config{Projects: []config.ProjectEntry{{Path: "~/existing/pattern"}}}
	data, _ := toml.Marshal(existingCfg)
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("failed to write existing config: %v", err)
	}

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = oldCfgFile }()

	var output bytes.Buffer
	d := &configureDeps{
		FS:      &deps.MockFileSystem{},
		Stdin:   strings.NewReader("n\n"),
		Stdout:  &output,
		PickDir: mockPickDirCancelled(),
	}

	err := runConfigureWith(d)
	if err != nil {
		t.Fatalf("runConfigureWith() error = %v", err)
	}

	out := output.String()
	if !strings.Contains(out, "Current patterns:") {
		t.Errorf("expected current patterns display, got: %s", out)
	}
	if strings.Contains(out, "Config written to") {
		t.Errorf("config should not be written when declining, got: %s", out)
	}
}

func TestRunConfigure_MultiplePatterns(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "pop", "config.toml")

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = oldCfgFile }()

	// "y" to add another, then "n" to stop
	var output bytes.Buffer
	d := &configureDeps{
		FS:      realFSDeps(),
		Stdin:   strings.NewReader("y\nn\n"),
		Stdout:  &output,
		PickDir: mockPickDirSequence("/first/dir", "/second/dir"),
	}

	err := runConfigureWith(d)
	if err != nil {
		t.Fatalf("runConfigureWith() error = %v", err)
	}

	written, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var cfg config.Config
	if err := toml.Unmarshal(written, &cfg); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if len(cfg.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(cfg.Projects), cfg.Projects)
	}
	if cfg.Projects[0].Path != "/first/dir/*" || cfg.Projects[1].Path != "/second/dir/*" {
		t.Errorf("unexpected projects: %v", cfg.Projects)
	}
}

func TestRunConfigure_PickerCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = oldCfgFile }()

	var output bytes.Buffer
	d := &configureDeps{
		FS:      &deps.MockFileSystem{},
		Stdin:   strings.NewReader(""),
		Stdout:  &output,
		PickDir: mockPickDirCancelled(),
	}

	err := runConfigureWith(d)
	if err != nil {
		t.Fatalf("runConfigureWith() error = %v", err)
	}

	if strings.Contains(output.String(), "Config written to") {
		t.Errorf("config should not be written when picker cancelled")
	}
}

func TestRunConfigure_WriteFails(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = oldCfgFile }()

	var output bytes.Buffer
	d := &configureDeps{
		FS: &deps.MockFileSystem{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return os.ErrPermission
			},
		},
		Stdin:   strings.NewReader("n\n"),
		Stdout:  &output,
		PickDir: mockPickDir("/some/path"),
	}

	err := runConfigureWith(d)
	if err == nil {
		t.Fatal("expected error when mkdir fails")
	}
	if !strings.Contains(err.Error(), "failed to create config directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunConfigure_TildePattern(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get home dir")
	}

	got := toTildePattern(home + "/Dev/personal")
	if got != "~/Dev/personal/*" {
		t.Errorf("toTildePattern() = %q, want %q", got, "~/Dev/personal/*")
	}

	got = toTildePattern("/opt/projects")
	if got != "/opt/projects/*" {
		t.Errorf("toTildePattern() = %q, want %q", got, "/opt/projects/*")
	}
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "yes lowercase", input: "y\n", expected: true},
		{name: "yes uppercase", input: "Y\n", expected: true},
		{name: "no", input: "n\n", expected: false},
		{name: "empty", input: "\n", expected: false},
		{name: "other", input: "maybe\n", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(tt.input))
			var buf bytes.Buffer
			result := confirm(scanner, &buf, "test?")
			if result != tt.expected {
				t.Errorf("confirm() = %v, want %v", result, tt.expected)
			}
		})
	}
}
