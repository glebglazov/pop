package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func mockExpandFn(paths []string) func(string) []string {
	return func(pattern string) []string {
		if pattern == "" {
			return nil
		}
		return paths
	}
}

func sendKeys(cp *ConfigurePicker, msgs ...tea.Msg) *ConfigurePicker {
	var m tea.Model = cp
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	return m.(*ConfigurePicker)
}

func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func specialKeyMsg(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func TestConfigurePicker_PathPhase_TypeCharsUpdatesPreview(t *testing.T) {
	paths := []string{"/home/user/Dev/foo", "/home/user/Dev/bar"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		keyMsg("~"),
		keyMsg("/"),
		keyMsg("D"),
	)

	if cp.phase != phasePath {
		t.Errorf("expected phasePath, got %d", cp.phase)
	}
	if len(cp.preview) != 2 {
		t.Errorf("expected 2 preview items, got %d", len(cp.preview))
	}
	if cp.preview[0] != "foo" {
		t.Errorf("expected preview[0] = 'foo', got %q", cp.preview[0])
	}
}

func TestConfigurePicker_PathPhase_EnterTransitionsToDepth(t *testing.T) {
	paths := []string{"/home/user/Dev/foo"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		keyMsg("~"),
		keyMsg("/"),
		keyMsg("D"),
		specialKeyMsg(tea.KeyEnter),
	)

	if cp.phase != phaseDepth {
		t.Errorf("expected phaseDepth, got %d", cp.phase)
	}
	if cp.path != "~/D" {
		t.Errorf("expected path '~/D', got %q", cp.path)
	}
	if cp.input.Value() != "1" {
		t.Errorf("expected input '1', got %q", cp.input.Value())
	}
}

func TestConfigurePicker_PathPhase_EscCancels(t *testing.T) {
	cp := NewConfigurePicker(mockExpandFn(nil))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		specialKeyMsg(tea.KeyEscape),
	)

	if !cp.cancelled {
		t.Error("expected cancelled = true")
	}
	result := cp.Result()
	if !result.Cancelled {
		t.Error("expected Result().Cancelled = true")
	}
}

func TestConfigurePicker_PathPhase_EmptyEnterCancels(t *testing.T) {
	cp := NewConfigurePicker(mockExpandFn(nil))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		specialKeyMsg(tea.KeyEnter),
	)

	if !cp.cancelled {
		t.Error("expected cancelled on empty enter")
	}
}

func TestConfigurePicker_DepthPhase_UpDownAdjustsDepth(t *testing.T) {
	paths := []string{"/a/b/c/foo", "/a/b/c/bar"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	// Type something and enter to get to depth phase
	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		keyMsg("x"),
		specialKeyMsg(tea.KeyEnter),
	)

	if cp.phase != phaseDepth {
		t.Fatalf("expected phaseDepth, got %d", cp.phase)
	}
	if cp.depth != 1 {
		t.Errorf("expected initial depth 1, got %d", cp.depth)
	}

	// Press Up
	cp = sendKeys(cp, specialKeyMsg(tea.KeyUp))
	if cp.depth != 2 {
		t.Errorf("expected depth 2 after Up, got %d", cp.depth)
	}
	if cp.preview[0] != "c/foo" {
		t.Errorf("expected preview 'c/foo' at depth 2, got %q", cp.preview[0])
	}

	// Press Down
	cp = sendKeys(cp, specialKeyMsg(tea.KeyDown))
	if cp.depth != 1 {
		t.Errorf("expected depth 1 after Down, got %d", cp.depth)
	}

	// Press Down at min (1) should not go below
	cp = sendKeys(cp, specialKeyMsg(tea.KeyDown))
	if cp.depth != 1 {
		t.Errorf("expected depth to stay at 1, got %d", cp.depth)
	}
}

func TestConfigurePicker_DepthPhase_EnterConfirms(t *testing.T) {
	paths := []string{"/a/b/c/foo"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		keyMsg("x"),
		specialKeyMsg(tea.KeyEnter),
		// Now in depth phase
		specialKeyMsg(tea.KeyUp), // depth = 2
		specialKeyMsg(tea.KeyEnter),
	)

	if !cp.confirmed {
		t.Error("expected confirmed = true")
	}
	result := cp.Result()
	if result.Cancelled {
		t.Error("expected not cancelled")
	}
	if result.Path != "x" {
		t.Errorf("expected path 'x', got %q", result.Path)
	}
	if result.DisplayDepth != 2 {
		t.Errorf("expected display depth 2, got %d", result.DisplayDepth)
	}
}

func TestConfigurePicker_DepthPhase_EscGoesBackToPath(t *testing.T) {
	paths := []string{"/a/b/foo"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		keyMsg("m"),
		keyMsg("y"),
		keyMsg("p"),
		specialKeyMsg(tea.KeyEnter),
		// Now in depth phase
		specialKeyMsg(tea.KeyUp), // depth = 2
		specialKeyMsg(tea.KeyEscape),
	)

	if cp.phase != phasePath {
		t.Errorf("expected phasePath after Esc, got %d", cp.phase)
	}
	if cp.input.Value() != "myp" {
		t.Errorf("expected input restored to 'myp', got %q", cp.input.Value())
	}
	if cp.depth != 2 {
		t.Errorf("expected depth preserved at 2, got %d", cp.depth)
	}
	// Cursor should be restored to end of "myp" (position 3)
	if cp.input.Position() != 3 {
		t.Errorf("expected cursor at position 3, got %d", cp.input.Position())
	}
}

func TestConfigurePicker_CursorMemoryAcrossPhases(t *testing.T) {
	paths := []string{"/a/b/foo"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	// Type "abcde", move cursor left twice (to position 3), then Enter
	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		keyMsg("a"),
		keyMsg("b"),
		keyMsg("c"),
		keyMsg("d"),
		keyMsg("e"),
		specialKeyMsg(tea.KeyLeft),
		specialKeyMsg(tea.KeyLeft),
	)

	pathCursorPos := cp.input.Position() // should be 3
	if pathCursorPos != 3 {
		t.Fatalf("expected path cursor at 3, got %d", pathCursorPos)
	}

	// Enter to go to depth phase
	cp = sendKeys(cp, specialKeyMsg(tea.KeyEnter))

	if cp.phase != phaseDepth {
		t.Fatalf("expected phaseDepth, got %d", cp.phase)
	}

	// Esc back to path phase — cursor should be at saved position 3
	cp = sendKeys(cp, specialKeyMsg(tea.KeyEscape))

	if cp.input.Position() != 3 {
		t.Errorf("expected cursor restored to 3, got %d", cp.input.Position())
	}

	// Enter again to depth, then move depth cursor
	cp = sendKeys(cp,
		specialKeyMsg(tea.KeyEnter),
		// In depth phase with "1", cursor at 0 (initial)
		specialKeyMsg(tea.KeyRight), // move to end
	)

	depthCursorPos := cp.input.Position()

	// Esc back
	cp = sendKeys(cp, specialKeyMsg(tea.KeyEscape))

	// Path cursor should still be at 3
	if cp.input.Position() != 3 {
		t.Errorf("expected path cursor at 3, got %d", cp.input.Position())
	}

	// Enter again — depth cursor should be restored
	cp = sendKeys(cp, specialKeyMsg(tea.KeyEnter))

	if cp.input.Position() != depthCursorPos {
		t.Errorf("expected depth cursor at %d, got %d", depthCursorPos, cp.input.Position())
	}
}

func TestConfigurePicker_DepthPhase_CtrlCCancels(t *testing.T) {
	paths := []string{"/a/b/foo"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		keyMsg("x"),
		specialKeyMsg(tea.KeyEnter),
		// Now in depth phase
		tea.KeyMsg{Type: tea.KeyCtrlC},
	)

	if !cp.cancelled {
		t.Error("expected cancelled on Ctrl+C in depth phase")
	}
}

func TestConfigurePicker_DepthPhase_IgnoresNonDigit(t *testing.T) {
	paths := []string{"/a/b/foo"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 80, Height: 24},
		keyMsg("x"),
		specialKeyMsg(tea.KeyEnter),
		// Now in depth phase, input = "1"
		keyMsg("a"), // should be ignored
	)

	if cp.input.Value() != "1" {
		t.Errorf("expected input to stay '1' after non-digit, got %q", cp.input.Value())
	}
}

func TestConfigurePicker_TabCompletion(t *testing.T) {
	// Create temp dirs for completion
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "alpha"), 0o755)
	os.MkdirAll(filepath.Join(tmpDir, "beta"), 0o755)
	os.MkdirAll(filepath.Join(tmpDir, "apex"), 0o755)
	os.MkdirAll(filepath.Join(tmpDir, ".hidden"), 0o755)

	cp := NewConfigurePicker(mockExpandFn(nil))

	// Type the temp dir path + "a"
	for _, r := range tmpDir + "/a" {
		cp = sendKeys(cp, keyMsg(string(r)))
	}

	// Press Tab
	cp = sendKeys(cp, specialKeyMsg(tea.KeyTab))

	val := cp.input.Value()
	// Should complete to one of the "a" dirs (alpha or apex, sorted alphabetically)
	if val != tmpDir+"/alpha/" && val != tmpDir+"/apex/" {
		t.Errorf("expected completion to alpha/ or apex/, got %q", val)
	}
	first := val

	// Tab again to cycle
	cp = sendKeys(cp, specialKeyMsg(tea.KeyTab))
	val = cp.input.Value()
	if val == first {
		t.Errorf("expected different completion on second Tab, got same %q", val)
	}

	// Type something to clear tab state, then Tab should recompute
	cp = sendKeys(cp, specialKeyMsg(tea.KeyBackspace))
	if cp.tabIndex != -1 {
		t.Errorf("expected tabIndex reset after keystroke, got %d", cp.tabIndex)
	}
}

func TestConfigurePicker_TabCompletion_Symlinks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a real directory and a symlink pointing to it
	realDir := filepath.Join(tmpDir, "realdir")
	os.MkdirAll(realDir, 0o755)
	symlinkDir := filepath.Join(tmpDir, "syml")
	os.Symlink(realDir, symlinkDir)

	// Create a regular file (should not appear in completion)
	os.WriteFile(filepath.Join(tmpDir, "somefile"), []byte("hi"), 0o644)

	// Create a symlink to a file (should not appear in completion)
	fileTarget := filepath.Join(tmpDir, "somefile")
	os.Symlink(fileTarget, filepath.Join(tmpDir, "symfile"))

	cp := NewConfigurePicker(mockExpandFn(nil))

	// Type tmpDir + "/" to list all entries
	for _, r := range tmpDir + "/" {
		cp = sendKeys(cp, keyMsg(string(r)))
	}

	// Press Tab — should complete, and both realdir and syml should be candidates
	cp = sendKeys(cp, specialKeyMsg(tea.KeyTab))

	if cp.tabIndex == -1 {
		t.Fatal("expected tab completion to find matches")
	}

	// Collect all matches by cycling through
	seen := make(map[string]bool)
	for i := 0; i < len(cp.tabMatches); i++ {
		seen[cp.tabMatches[i]] = true
	}

	if !seen["realdir"] {
		t.Error("expected 'realdir' in tab matches")
	}
	if !seen["syml"] {
		t.Error("expected 'syml' (symlink to dir) in tab matches")
	}
	if seen["somefile"] {
		t.Error("'somefile' (regular file) should not be in tab matches")
	}
	if seen["symfile"] {
		t.Error("'symfile' (symlink to file) should not be in tab matches")
	}
}

func TestConfigurePicker_TabCompletion_AfterStar(t *testing.T) {
	cp := NewConfigurePicker(mockExpandFn(nil))

	// Type "~/Dev/*"
	for _, r := range "~/Dev/*" {
		cp = sendKeys(cp, keyMsg(string(r)))
	}

	// Press Tab — should insert "/"
	cp = sendKeys(cp, specialKeyMsg(tea.KeyTab))

	val := cp.input.Value()
	if val != "~/Dev/*/" {
		t.Errorf("expected '~/Dev/*/' after Tab on *, got %q", val)
	}

	// Tab state should not be active (this wasn't directory completion)
	if cp.tabIndex != -1 {
		t.Errorf("expected tabIndex -1 after star-slash insert, got %d", cp.tabIndex)
	}
}

func TestConfigurePicker_TabCompletion_NoMatches(t *testing.T) {
	tmpDir := t.TempDir()

	cp := NewConfigurePicker(mockExpandFn(nil))

	for _, r := range tmpDir + "/zzz_nonexistent" {
		cp = sendKeys(cp, keyMsg(string(r)))
	}

	cp = sendKeys(cp, specialKeyMsg(tea.KeyTab))

	// Should not crash, tabIndex stays -1
	if cp.tabIndex != -1 {
		t.Errorf("expected tabIndex -1 with no matches, got %d", cp.tabIndex)
	}
}

func TestConfigurePicker_View_ShowsPreview(t *testing.T) {
	paths := []string{"/home/user/foo", "/home/user/bar"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 60, Height: 20},
		keyMsg("x"),
	)

	view := cp.View()
	if !containsText(view, "Preview:") {
		t.Error("expected 'Preview:' in view")
	}
	if !containsText(view, "foo") {
		t.Error("expected 'foo' in preview")
	}
	if !containsText(view, "bar") {
		t.Error("expected 'bar' in preview")
	}
}

func TestConfigurePicker_View_ShowsDepthInPreviewHeader(t *testing.T) {
	paths := []string{"/a/b/foo"}
	cp := NewConfigurePicker(mockExpandFn(paths))

	cp = sendKeys(cp,
		tea.WindowSizeMsg{Width: 60, Height: 20},
		keyMsg("x"),
		specialKeyMsg(tea.KeyEnter),
		specialKeyMsg(tea.KeyUp), // depth = 2
	)

	view := cp.View()
	if !containsText(view, "depth: 2") {
		t.Error("expected 'depth: 2' in view")
	}
}

func containsText(s, substr string) bool {
	// Strip ANSI codes for comparison
	return len(s) > 0 && contains(s, substr)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
