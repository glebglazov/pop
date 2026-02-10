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
	"github.com/glebglazov/pop/ui"
)

func mockPickDir(path string, depth int) func() (ui.ConfigurePickerResult, error) {
	return func() (ui.ConfigurePickerResult, error) {
		return ui.ConfigurePickerResult{Path: path, DisplayDepth: depth}, nil
	}
}

func mockPickDirCancelled() func() (ui.ConfigurePickerResult, error) {
	return func() (ui.ConfigurePickerResult, error) {
		return ui.ConfigurePickerResult{Cancelled: true}, nil
	}
}

func mockPickDirSequence(entries ...ui.ConfigurePickerResult) func() (ui.ConfigurePickerResult, error) {
	i := 0
	return func() (ui.ConfigurePickerResult, error) {
		if i >= len(entries) {
			return ui.ConfigurePickerResult{Cancelled: true}, nil
		}
		entry := entries[i]
		i++
		return entry, nil
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
		PickDir: mockPickDir("/fake/projects/*", 1),
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
		PickDir: mockPickDir("/new/projects/*", 1),
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
		FS:     realFSDeps(),
		Stdin:  strings.NewReader("y\nn\n"),
		Stdout: &output,
		PickDir: mockPickDirSequence(
			ui.ConfigurePickerResult{Path: "/first/dir/*", DisplayDepth: 1},
			ui.ConfigurePickerResult{Path: "/second/dir/*", DisplayDepth: 1},
		),
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
		PickDir: mockPickDir("/some/path/*", 1),
	}

	err := runConfigureWith(d)
	if err == nil {
		t.Fatal("expected error when mkdir fails")
	}
	if !strings.Contains(err.Error(), "failed to create config directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunConfigure_DisplayDepthInConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "pop", "config.toml")

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = oldCfgFile }()

	var output bytes.Buffer
	d := &configureDeps{
		FS:      realFSDeps(),
		Stdin:   strings.NewReader("n\n"),
		Stdout:  &output,
		PickDir: mockPickDir("~/Dev/*/*", 2),
	}

	err := runConfigureWith(d)
	if err != nil {
		t.Fatalf("runConfigureWith() error = %v", err)
	}

	out := output.String()
	if !strings.Contains(out, "(depth: 2)") {
		t.Errorf("expected depth info in output, got: %s", out)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var cfg config.Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if len(cfg.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].Path != "~/Dev/*/*" {
		t.Errorf("expected path ~/Dev/*/*, got %s", cfg.Projects[0].Path)
	}
	if cfg.Projects[0].DisplayDepth != 2 {
		t.Errorf("expected display_depth 2, got %d", cfg.Projects[0].DisplayDepth)
	}
}

func TestRunConfigure_DisplayDepthDefaultNotShown(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "pop", "config.toml")

	oldCfgFile := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = oldCfgFile }()

	var output bytes.Buffer
	d := &configureDeps{
		FS:      realFSDeps(),
		Stdin:   strings.NewReader("n\n"),
		Stdout:  &output,
		PickDir: mockPickDir("~/Dev/*", 1),
	}

	err := runConfigureWith(d)
	if err != nil {
		t.Fatalf("runConfigureWith() error = %v", err)
	}

	out := output.String()
	if strings.Contains(out, "depth:") {
		t.Errorf("depth info should not appear for depth=1, got: %s", out)
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
