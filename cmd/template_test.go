package cmd

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

func TestTemplateCommandTree(t *testing.T) {
	tests := []struct {
		path    []string
		wantCmd any
		wantRun any
	}{
		// New canonical paths
		{path: []string{"workbench", "list"}, wantCmd: workbenchListCmd, wantRun: runTemplateList},
		{path: []string{"workbench", "apply"}, wantCmd: workbenchApplyCmd, wantRun: runTemplateApply},
		// Alias
		{path: []string{"wb", "list"}, wantCmd: workbenchListCmd, wantRun: runTemplateList},
		{path: []string{"wb", "apply"}, wantCmd: workbenchApplyCmd, wantRun: runTemplateApply},
		// Deprecated hidden alias still works
		{path: []string{"layout", "list"}, wantCmd: layoutListCmd, wantRun: runTemplateList},
		{path: []string{"layout", "apply"}, wantCmd: layoutApplyCmd, wantRun: runTemplateApply},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.path, " "), func(t *testing.T) {
			got, _, err := rootCmd.Find(tt.path)
			if err != nil {
				t.Fatalf("Find(%v): %v", tt.path, err)
			}
			if got != tt.wantCmd {
				t.Fatalf("Find(%v) = %q, want template subcommand", tt.path, got.CommandPath())
			}
			if reflect.ValueOf(got.RunE).Pointer() != reflect.ValueOf(tt.wantRun).Pointer() {
				t.Fatalf("%q does not use the expected handler", got.CommandPath())
			}
		})
	}
}

func TestWorkbenchCmdIsVisibleLayoutCmdIsHidden(t *testing.T) {
	if workbenchCmd.Hidden {
		t.Fatal("workbench command should not be hidden")
	}
	if !layoutCmd.Hidden {
		t.Fatal("layout command must be hidden (deprecated alias)")
	}
}

func TestRunTemplateListWith(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{
			{Name: "dev"},
			{Name: "review"},
		},
	}
	var out bytes.Buffer

	if err := runTemplateListWith(cfg.SessionTemplates, &out); err != nil {
		t.Fatalf("runTemplateListWith() error: %v", err)
	}
	if got, want := out.String(), "dev\nreview\n"; got != want {
		t.Fatalf("list output = %q, want %q", got, want)
	}
}

func TestRunTemplateApplyWith(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "dev",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "new-window":
				return "%42", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:   tmux,
		Getwd:  func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut: io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	want := [][]string{
		{"display-message", "-p", "#S"},
		{"list-windows", "-t", "current-session", "-F", "#{@pop_wb_window}\t#{window_id}"},
		{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session:", "-n", "work", "-c", "/repo/checkout"},
		{"set-option", "-w", "-t", "current-session:work", "@pop_wb_window", "work"},
		{"set-option", "-w", "-t", "current-session:work", "automatic-rename", "off"},
		{"select-pane", "-t", "%42", "-T", "server"},
		{"set-option", "-p", "-t", "%42", "@pop_pane", "server"},
		{"send-keys", "-t", "%42", "go test ./...", "Enter"},
		{"select-window", "-t", "current-session:work"},
		{"select-pane", "-t", "%42"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}

func TestRunTemplateApplyWithUnknownName(t *testing.T) {
	err := runTemplateApplyWith(templateRuntimeDeps{Tmux: &deps.MockTmux{}}, []config.SessionTemplate{}, "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `session template "missing" not found`) {
		t.Fatalf("error = %q, want clear unknown-template message", err.Error())
	}
}

func TestRunTemplateApplyWithTmuxFailure(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "dev",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "display-message" {
				return "current-session", nil
			}
			if args[0] == "new-window" {
				return "", fmt.Errorf("tmux refused")
			}
			return "", nil
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	err := runTemplateApplyWith(d, cfg.SessionTemplates, "dev")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `failed to create template window "work"`) {
		t.Fatalf("error = %q, want window creation context", err.Error())
	}
}

func TestRunTemplateApplyWithFlatWeightedSplits(t *testing.T) {
	// Test: window with 3 panes in a row with weights 1, 2, 3
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "weighted",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "columns",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "left", Command: "echo left", Weight: 1},
						{Name: "middle", Command: "echo middle", Weight: 2},
						{Name: "right", Command: "echo right", Weight: 3},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "120", nil
					}
					if args[2] == "#{window_height}" {
						return "40", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				// Return incrementing pane IDs
				paneNum := len(calls)
				return fmt.Sprintf("%%%d", paneNum), nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "weighted"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Verify split-window calls used -h flag (row = side-by-side)
	splitCount := 0
	for _, call := range calls {
		if call[0] == "split-window" {
			splitCount++
			if call[1] != "-h" {
				t.Errorf("split-window call %v should use -h for row direction", call)
			}
		}
	}
	if splitCount != 2 {
		t.Errorf("expected 2 split-window calls, got %d", splitCount)
	}

	// Verify resize-pane calls
	resizeCount := 0
	for _, call := range calls {
		if call[0] == "resize-pane" {
			resizeCount++
			// Should use -x flag for row direction
			if call[3] != "-x" {
				t.Errorf("resize-pane call %v should use -x for row direction", call)
			}
		}
	}
	if resizeCount != 3 {
		t.Errorf("expected 3 resize-pane calls, got %d", resizeCount)
	}
}

func TestRunTemplateApplyWithColumnDirection(t *testing.T) {
	// Test: column direction uses -v for splits and -y for resize
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "stacked",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "rows",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "top", Command: "echo top"},
						{Name: "bottom", Command: "echo bottom"},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "120", nil
					}
					if args[2] == "#{window_height}" {
						return "40", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				return "%1", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "stacked"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Verify split-window used -v flag (column = stacked)
	foundSplit := false
	for _, call := range calls {
		if call[0] == "split-window" {
			foundSplit = true
			if call[1] != "-v" {
				t.Errorf("split-window call %v should use -v for column direction", call)
			}
		}
	}
	if !foundSplit {
		t.Error("expected at least one split-window call")
	}

	// Verify resize-pane used -y flag for column direction
	foundResize := false
	for _, call := range calls {
		if call[0] == "resize-pane" {
			foundResize = true
			if call[3] != "-y" {
				t.Errorf("resize-pane call %v should use -y for column direction", call)
			}
		}
	}
	if !foundResize {
		t.Error("expected at least one resize-pane call")
	}
}

func TestRunTemplateApplyWithNestedContainers(t *testing.T) {
	// Test: nested containers - outer row with 2 children, first child is a column container
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "nested",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "columns",
					Panes: []config.SessionTemplatePaneSpec{
						{
							Children: "rows",
							Weight:   1,
							Panes: []config.SessionTemplatePaneSpec{
								{Name: "top-left", Command: "echo tl", Weight: 1},
								{Name: "bottom-left", Command: "echo bl", Weight: 1},
							},
						},
						{Name: "right", Command: "echo right", Weight: 1},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "120", nil
					}
					if args[2] == "#{window_height}" {
						return "40", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				paneNum := len(calls)
				return fmt.Sprintf("%%%d", paneNum), nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "nested"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Should have split-window calls for both outer and inner containers
	splitCount := 0
	for _, call := range calls {
		if call[0] == "split-window" {
			splitCount++
		}
	}
	// Outer container splits once (2 children), inner container splits once (2 children)
	if splitCount < 2 {
		t.Errorf("expected at least 2 split-window calls for nested containers, got %d", splitCount)
	}
}

func TestRunTemplateApplyWithDefaultWeight(t *testing.T) {
	// Test: omitted weight defaults to 1 (equal split)
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "equal",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "columns",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "left", Command: "echo left"},   // weight omitted = 1
						{Name: "right", Command: "echo right"}, // weight omitted = 1
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "100", nil
					}
					if args[2] == "#{window_height}" {
						return "50", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				return "%1", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "equal"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Verify resize-pane calls with equal sizes (50 each for 100 width)
	resizeCalls := []int{}
	for _, call := range calls {
		if call[0] == "resize-pane" && call[3] == "-x" {
			var size int
			fmt.Sscanf(call[4], "%d", &size)
			resizeCalls = append(resizeCalls, size)
		}
	}
	if len(resizeCalls) != 2 {
		t.Fatalf("expected 2 resize-pane calls, got %d", len(resizeCalls))
	}
	// Both should be 50 (equal split of 100)
	if resizeCalls[0] != 50 || resizeCalls[1] != 50 {
		t.Errorf("expected equal sizes [50, 50], got %v", resizeCalls)
	}
}

func TestRunTemplateApplyWithDeepNesting(t *testing.T) {
	// Test: 3 levels deep nesting
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "deep",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "columns",
					Panes: []config.SessionTemplatePaneSpec{
						{
							Children: "rows",
							Panes: []config.SessionTemplatePaneSpec{
								{
									Children: "columns",
									Panes: []config.SessionTemplatePaneSpec{
										{Name: "deep-left", Command: "echo dl"},
										{Name: "deep-right", Command: "echo dr"},
									},
								},
								{Name: "bottom", Command: "echo bottom"},
							},
						},
						{Name: "right", Command: "echo right"},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 1 && args[1] == "-p" {
					if args[2] == "#{window_width}" {
						return "120", nil
					}
					if args[2] == "#{window_height}" {
						return "40", nil
					}
				}
				return "current-session", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				paneNum := len(calls)
				return fmt.Sprintf("%%%d", paneNum), nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:  tmux,
		Getwd: func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "deep"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Should successfully create all panes at all nesting levels
	// Count send-keys calls (one per leaf pane)
	sendKeysCount := 0
	for _, call := range calls {
		if call[0] == "send-keys" {
			sendKeysCount++
		}
	}
	// 4 leaf panes: deep-left, deep-right, bottom, right
	if sendKeysCount != 4 {
		t.Errorf("expected 4 send-keys calls for 4 leaf panes, got %d", sendKeysCount)
	}
}

func TestRunTemplateApplyWithMultipleWindows(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "dev",
			Windows: []config.SessionTemplateWindow{
				{Name: "work", Layout: &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."}},
				{Name: "logs", Layout: &config.SessionTemplatePaneSpec{Name: "tail", Command: "tail -f app.log"}},
			},
		}},
	}
	var calls [][]string
	newWindowCount := 0
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "list-windows":
				return "", nil
			case "new-window":
				id := fmt.Sprintf("%%%d", newWindowCount)
				newWindowCount++
				return id, nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:   tmux,
		Getwd:  func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut: io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	want := [][]string{
		{"display-message", "-p", "#S"},
		{"list-windows", "-t", "current-session", "-F", "#{@pop_wb_window}\t#{window_id}"},
		{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session:", "-n", "work", "-c", "/repo/checkout"},
		{"set-option", "-w", "-t", "current-session:work", "@pop_wb_window", "work"},
		{"set-option", "-w", "-t", "current-session:work", "automatic-rename", "off"},
		{"select-pane", "-t", "%0", "-T", "server"},
		{"set-option", "-p", "-t", "%0", "@pop_pane", "server"},
		{"send-keys", "-t", "%0", "go test ./...", "Enter"},
		{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session:", "-n", "logs", "-c", "/repo/checkout"},
		{"set-option", "-w", "-t", "current-session:logs", "@pop_wb_window", "logs"},
		{"set-option", "-w", "-t", "current-session:logs", "automatic-rename", "off"},
		{"select-pane", "-t", "%1", "-T", "tail"},
		{"set-option", "-p", "-t", "%1", "@pop_pane", "tail"},
		{"send-keys", "-t", "%1", "tail -f app.log", "Enter"},
		{"select-window", "-t", "current-session:work"},
		{"select-pane", "-t", "%0"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}

func TestRunTemplateApplyWithUnstampedWindowCreatesFresh(t *testing.T) {
	// A live window whose display name happens to equal a target window but
	// which carries no @pop_wb_window stamp is NOT matched (identity never lives
	// in window_name, ADR-0075): the target window is created fresh.
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "dev",
			Windows: []config.SessionTemplateWindow{
				{Name: "work", Layout: &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."}},
			},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "list-windows":
				// One live window with display name "work" but no stamp:
				// empty @pop_wb_window field before the tab.
				return "\t@5\n", nil
			case "new-window":
				return "%0", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// The window is built fresh (no merge, no list-panes inspection).
	want := [][]string{
		{"display-message", "-p", "#S"},
		{"list-windows", "-t", "current-session", "-F", "#{@pop_wb_window}\t#{window_id}"},
		{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session:", "-n", "work", "-c", "/repo/checkout"},
		{"set-option", "-w", "-t", "current-session:work", "@pop_wb_window", "work"},
		{"set-option", "-w", "-t", "current-session:work", "automatic-rename", "off"},
		{"select-pane", "-t", "%0", "-T", "server"},
		{"set-option", "-p", "-t", "%0", "@pop_pane", "server"},
		{"send-keys", "-t", "%0", "go test ./...", "Enter"},
		{"select-window", "-t", "current-session:work"},
		{"select-pane", "-t", "%0"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("tmux calls = %#v, want %#v", calls, want)
	}
}

func TestEffectiveCwdAndResolveCwd(t *testing.T) {
	tests := []struct {
		name       string
		sessionDir string
		parentCwd  string
		rawCwd     string
		homeDir    string
		want       string
	}{
		{
			name:       "inherit parent cwd when empty",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "",
			homeDir:    "/home/user",
			want:       "/repo/backend",
		},
		{
			name:       "relative path resolves under session dir",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "api",
			homeDir:    "/home/user",
			want:       "/repo/api",
		},
		{
			name:       "absolute path preserved",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "/tmp",
			homeDir:    "/home/user",
			want:       "/tmp",
		},
		{
			name:       "tilde expands to home",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "~/docs",
			homeDir:    "/home/user",
			want:       "/home/user/docs",
		},
		{
			name:       "tilde only without slash is literal",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "~docs",
			homeDir:    "/home/user",
			want:       "/repo/~docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveCwd(tt.sessionDir, tt.parentCwd, tt.rawCwd, tt.homeDir)
			if got != tt.want {
				t.Fatalf("effectiveCwd() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunTemplateApplyWithCwdInheritanceAndOverride(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "cwd-test",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "rows",
					Cwd:      "backend",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "inherited", Command: "echo inherited"},
						{Name: "override", Command: "echo override", Cwd: "api"},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "list-windows":
				return "", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				return "%1", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "cwd-test"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// The window should be created in the container's cwd.
	assertContainsCall(t, calls, []string{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "current-session:", "-n", "work", "-c", "/repo/backend"})
	// The override pane should be split into its own cwd.
	assertContainsCall(t, calls, []string{"split-window", "-v", "-t", "%0", "-p", "50", "-P", "-F", "#{pane_id}", "-c", "/repo/api"})
	// No respawn-pane is needed because the first child inherits.
	for _, call := range calls {
		if call[0] == "respawn-pane" {
			t.Fatalf("unexpected respawn-pane call: %v", call)
		}
	}
}

func TestRunTemplateApplyWithCwdTildeAndAbsolute(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "cwd-test",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "rows",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "home", Command: "echo home", Cwd: "~/docs"},
						{Name: "abs", Command: "echo abs", Cwd: "/tmp"},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "list-windows":
				return "", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				return "%1", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "cwd-test"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	assertContainsCall(t, calls, []string{"respawn-pane", "-c", "/home/user/docs", "-t", "%0", "-k"})
	assertContainsCall(t, calls, []string{"split-window", "-v", "-t", "%0", "-p", "50", "-P", "-F", "#{pane_id}", "-c", "/tmp"})
}

func TestRunTemplateApplyWithFocusOverride(t *testing.T) {
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "focus-test",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "columns",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "left", Command: "echo left"},
						{Name: "right", Command: "echo right", Focus: true},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "list-windows":
				return "", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				return "%1", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "focus-test"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// The focused pane should be the second one.
	assertContainsCall(t, calls, []string{"select-window", "-t", "current-session:work"})
	assertContainsCall(t, calls, []string{"select-pane", "-t", "%1"})
	// The first leaf pane should not be selected.
	assertNotFollowedBy(t, calls, []string{"select-window", "-t", "current-session:work"}, []string{"select-pane", "-t", "%0"})
}

func TestRunTemplateApplyWithMultipleFocusWarning(t *testing.T) {
	var warnings bytes.Buffer
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "focus-test",
			Windows: []config.SessionTemplateWindow{{
				Name: "work",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "columns",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "first", Command: "echo first", Focus: true},
						{Name: "second", Command: "echo second", Focus: true},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				return "current-session", nil
			case "list-windows":
				return "", nil
			case "new-window":
				return "%0", nil
			case "split-window":
				return "%1", nil
			default:
				return "", nil
			}
		},
	}
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      &warnings,
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "focus-test"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	warnStr := warnings.String()
	if !strings.Contains(warnStr, "multiple panes requested focus") {
		t.Fatalf("expected multiple-focus warning, got %q", warnStr)
	}
	// First focus wins: the initial pane (%0) is focused, not the split pane.
	assertContainsCall(t, calls, []string{"select-pane", "-t", "%0"})
	assertNotContainsCall(t, calls, []string{"select-pane", "-t", "%1"})
}

func TestRealizePaneTreeStampsNamedLeafSkipsUnnamed(t *testing.T) {
	var calls [][]string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			return "", nil
		},
	}

	// Named leaf: identity is stamped.
	calls = nil
	named := &config.SessionTemplatePaneSpec{Name: "server", Command: "go test ./..."}
	if _, err := realizePaneTree(tmux, named, "%7", "/repo", "/repo", "/home/user"); err != nil {
		t.Fatalf("realizePaneTree(named) error: %v", err)
	}
	assertContainsCall(t, calls, []string{"select-pane", "-t", "%7", "-T", "server"})
	assertContainsCall(t, calls, []string{"set-option", "-p", "-t", "%7", "@pop_pane", "server"})

	// Unnamed leaf: no @pop_pane stamp.
	calls = nil
	unnamed := &config.SessionTemplatePaneSpec{Command: "htop"}
	if _, err := realizePaneTree(tmux, unnamed, "%8", "/repo", "/repo", "/home/user"); err != nil {
		t.Fatalf("realizePaneTree(unnamed) error: %v", err)
	}
	for _, call := range calls {
		if len(call) >= 5 && call[0] == "set-option" && call[1] == "-p" && call[4] == "@pop_pane" {
			t.Fatalf("unnamed leaf must not be stamped with @pop_pane, got %v", call)
		}
	}
}

// mergeMockTmux returns a MockTmux for merge tests: it reports the live window
// `dev` (id @1) carrying the given @pop_pane→pane_id panes, fixed window
// dimensions, and deterministic split-window ids starting at %10.
func mergeMockTmux(calls *[][]string, livePanes, width, height string) *deps.MockTmux {
	splitN := 0
	return &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			*calls = append(*calls, append([]string(nil), args...))
			switch args[0] {
			case "display-message":
				if len(args) > 2 && args[1] == "-p" {
					switch args[2] {
					case "#{window_width}":
						return width, nil
					case "#{window_height}":
						return height, nil
					}
				}
				return "current-session", nil
			case "list-windows":
				return "dev\t@1\n", nil
			case "list-panes":
				return livePanes, nil
			case "split-window":
				splitN++
				return fmt.Sprintf("%%%d", 9+splitN), nil
			default:
				return "", nil
			}
		},
	}
}

func sentCommands(calls [][]string) map[string]bool {
	sent := make(map[string]bool)
	for _, call := range calls {
		if call[0] == "send-keys" && len(call) >= 4 {
			sent[call[3]] = true
		}
	}
	return sent
}

func sentToPane(calls [][]string, paneID string) bool {
	for _, call := range calls {
		if call[0] == "send-keys" && len(call) >= 3 && call[2] == paneID {
			return true
		}
	}
	return false
}

func TestRunTemplateApplyMergeSupersetAppend(t *testing.T) {
	// Reference transition (ADR-0075): a live window shaped by `minimal`
	// (rows: vim, claude) reapplied with `gs-dev` (those two rows plus a third
	// row of three columns) keeps the live vim/claude panes and appends only
	// the third row.
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "gs-dev",
			Windows: []config.SessionTemplateWindow{{
				Name: "dev",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "rows",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "vim", Command: "vim"},
						{Name: "claude", Command: "claude"},
						{
							Children: "columns",
							Panes: []config.SessionTemplatePaneSpec{
								{Name: "build", Command: "echo build"},
								{Name: "services", Command: "echo services"},
								{Name: "vite", Command: "echo vite"},
							},
						},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := mergeMockTmux(&calls, "vim\t%1\nclaude\t%2\n", "200", "60")
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "gs-dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Live vim/claude panes survive: their commands are never re-sent (process
	// intact) and they are never respawned or killed.
	if sentToPane(calls, "%1") {
		t.Error("vim pane %1 should be left untouched, but a command was sent to it")
	}
	if sentToPane(calls, "%2") {
		t.Error("claude pane %2 should be left untouched, but a command was sent to it")
	}
	for _, call := range calls {
		if call[0] == "respawn-pane" || call[0] == "kill-pane" {
			t.Fatalf("merge must never respawn or kill panes, got %v", call)
		}
	}

	// The third row is appended by splitting -v off the live claude pane (%2).
	assertContainsCall(t, calls, []string{"split-window", "-v", "-t", "%2", "-P", "-F", "#{pane_id}", "-c", "/repo"})

	// Only the third row's three panes are built (commands sent + identity stamped).
	sent := sentCommands(calls)
	for _, cmd := range []string{"echo build", "echo services", "echo vite"} {
		if !sent[cmd] {
			t.Errorf("expected the appended row to run %q", cmd)
		}
	}
	if sent["vim"] || sent["claude"] {
		t.Error("survivor commands must not be re-run on reapply")
	}
	for _, name := range []string{"build", "services", "vite"} {
		found := false
		for _, call := range calls {
			if call[0] == "set-option" && len(call) >= 6 && call[1] == "-p" && call[4] == "@pop_pane" && call[5] == name {
				found = true
			}
		}
		if !found {
			t.Errorf("appended pane %q should be stamped with @pop_pane", name)
		}
	}

	// Survivors are reproportioned to the target weights (three equal rows of a
	// 60-cell window = 20 cells each); no pane is killed.
	assertContainsCall(t, calls, []string{"resize-pane", "-t", "%1", "-y", "20"})
	assertContainsCall(t, calls, []string{"resize-pane", "-t", "%2", "-y", "20"})
}

func TestRunTemplateApplyMergeMidRowColumnInsertion(t *testing.T) {
	// A missing column inside an otherwise-live row is spliced in beside its
	// live left sibling, in the correct position.
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "row",
			Windows: []config.SessionTemplateWindow{{
				Name: "dev",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "columns",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "left", Command: "echo left"},
						{Name: "middle", Command: "echo middle"},
						{Name: "right", Command: "echo right"},
					},
				},
			}},
		}},
	}
	var calls [][]string
	// middle is missing; left=%1, right=%2 are live.
	tmux := mergeMockTmux(&calls, "left\t%1\nright\t%2\n", "90", "30")
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "row"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// middle is inserted by splitting -h off the live left pane (%1), so it
	// lands between left and right rather than after right.
	assertContainsCall(t, calls, []string{"split-window", "-h", "-t", "%1", "-P", "-F", "#{pane_id}", "-c", "/repo"})

	sent := sentCommands(calls)
	if !sent["echo middle"] {
		t.Error("the missing middle column should be created")
	}
	if sent["echo left"] || sent["echo right"] {
		t.Error("live left/right columns must not be re-run")
	}
	if sentToPane(calls, "%1") || sentToPane(calls, "%2") {
		t.Error("live columns must be left untouched")
	}
}

func TestRunTemplateApplyMergeReproportionsSurvivors(t *testing.T) {
	// Reapplying with new weights reproportions the surviving panes without
	// re-running or killing them.
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "grow",
			Windows: []config.SessionTemplateWindow{{
				Name: "dev",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "rows",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "vim", Command: "vim", Weight: 3},
						{Name: "claude", Command: "claude", Weight: 1},
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := mergeMockTmux(&calls, "vim\t%1\nclaude\t%2\n", "100", "80")
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "grow"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// No panes are added (both survive) and none are re-run.
	for _, call := range calls {
		if call[0] == "split-window" {
			t.Fatalf("no split expected when all panes survive, got %v", call)
		}
		if call[0] == "send-keys" {
			t.Fatalf("survivors must not be re-run, got %v", call)
		}
	}

	// vim weight 3 / claude weight 1 over an 80-cell window → 60 / 20 cells.
	assertContainsCall(t, calls, []string{"resize-pane", "-t", "%1", "-y", "60"})
	assertContainsCall(t, calls, []string{"resize-pane", "-t", "%2", "-y", "20"})
}

func TestRunTemplateApplyMergeRecreatesUnnamedLeaf(t *testing.T) {
	// Unnamed leaves are anonymous (ADR-0075 B1): with no identity they cannot
	// be matched, so a reapply always (re)creates them — even when a named
	// sibling survives.
	cfg := &config.Config{
		SessionTemplates: []config.SessionTemplate{{
			Name: "mixed",
			Windows: []config.SessionTemplateWindow{{
				Name: "dev",
				Layout: &config.SessionTemplatePaneSpec{
					Children: "rows",
					Panes: []config.SessionTemplatePaneSpec{
						{Name: "vim", Command: "vim"},
						{Command: "htop"}, // unnamed leaf
					},
				},
			}},
		}},
	}
	var calls [][]string
	tmux := mergeMockTmux(&calls, "vim\t%1\n", "100", "40")
	d := templateRuntimeDeps{
		Tmux:        tmux,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.SessionTemplates, "mixed"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// The unnamed leaf is appended (split off the live vim pane) and its
	// command run; vim itself is untouched.
	assertContainsCall(t, calls, []string{"split-window", "-v", "-t", "%1", "-P", "-F", "#{pane_id}", "-c", "/repo"})
	if !sentCommands(calls)["htop"] {
		t.Error("unnamed leaf should be recreated on reapply")
	}
	if sentToPane(calls, "%1") {
		t.Error("live vim pane must be left untouched")
	}
	// The recreated unnamed leaf is never stamped with @pop_pane.
	for _, call := range calls {
		if call[0] == "set-option" && len(call) >= 5 && call[1] == "-p" && call[4] == "@pop_pane" {
			t.Fatalf("unnamed leaf must not be stamped, got %v", call)
		}
	}
}

func assertContainsCall(t *testing.T, calls [][]string, want []string) {
	t.Helper()
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			return
		}
	}
	t.Fatalf("expected call %v not found in %v", want, calls)
}

func assertNotContainsCall(t *testing.T, calls [][]string, want []string) {
	t.Helper()
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			t.Fatalf("unexpected call %v found in %v", want, calls)
		}
	}
}

func assertNotFollowedBy(t *testing.T, calls [][]string, first, second []string) {
	t.Helper()
	for i := 0; i < len(calls)-1; i++ {
		if reflect.DeepEqual(calls[i], first) && reflect.DeepEqual(calls[i+1], second) {
			t.Fatalf("call %v was unexpectedly followed by %v", first, second)
		}
	}
}
