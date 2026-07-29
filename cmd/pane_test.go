package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/project"
)

// withTmuxMod swaps the package-level tmux module handle for the duration of a
// test, restoring it afterwards. resolveSessionWith's session-creation path
// (--project) goes through defaultTmuxMod (ADR-0142), so tests that exercise
// it inject a stateful fake here.
func withTmuxMod(t *testing.T, f *tmuxtest.Fake) {
	t.Helper()
	prev := defaultTmuxMod
	defaultTmuxMod = f
	t.Cleanup(func() { defaultTmuxMod = prev })
}

func TestFindPaneWith(t *testing.T) {
	t.Parallel()
	// Arrange the shared pane window's panes as fake state; the module owns the
	// list-panes construction, so the test asserts on state, not arg vectors.
	mod := &tmuxtest.Fake{
		Windows: map[string]map[string][]string{
			"project": {spawnWindow: {"%5", "%6", "%7"}},
		},
		PaneTitles: map[string]string{"%5": "server", "%6": "db", "%7": "logs"},
	}

	t.Run("finds existing pane", func(t *testing.T) {
		paneID, err := findPaneWith(mod, "project", "db")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if paneID != "%6" {
			t.Errorf("got %q, want %%6", paneID)
		}
	})

	t.Run("returns error for missing pane", func(t *testing.T) {
		_, err := findPaneWith(mod, "project", "nonexistent")
		if err == nil {
			t.Error("expected error for missing pane")
		}
	})
}

func TestRunPaneSendToPaneIDWith(t *testing.T) {
	t.Parallel()
	mod := &tmuxtest.Fake{}

	if err := runPaneSendToPaneIDWith(mod, "%63", []string{"hello", "Enter"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sent := mod.SentKeys["%63"]
	if len(sent) != 1 {
		t.Fatalf("send-keys calls for %%63 = %v, want 1", sent)
	}
	want := []string{"hello", "Enter"}
	if strings.Join(sent[0], "\x00") != strings.Join(want, "\x00") {
		t.Errorf("keys sent = %v, want %v", sent[0], want)
	}

	t.Run("requires keys", func(t *testing.T) {
		if err := runPaneSendToPaneIDWith(mod, "%63", nil); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestHasSpawnWindowWith(t *testing.T) {
	t.Parallel()
	t.Run("Spawn window exists", func(t *testing.T) {
		mod := &tmuxtest.Fake{
			Windows: map[string]map[string][]string{
				"project": {spawnWindow: {"%5"}},
			},
			PaneTitles: map[string]string{"%5": "server"},
		}
		if !hasSpawnWindowWith(mod, "project") {
			t.Error("expected Spawn window to exist")
		}
	})

	t.Run("no Spawn window", func(t *testing.T) {
		mod := &tmuxtest.Fake{}
		if hasSpawnWindowWith(mod, "project") {
			t.Error("expected no Spawn window")
		}
	})

	t.Run("legacy agent window does not count as Spawn window", func(t *testing.T) {
		mod := &tmuxtest.Fake{
			Windows: map[string]map[string][]string{
				"project": {"agent": {"%5"}},
			},
			PaneTitles: map[string]string{"%5": "server"},
		}
		if hasSpawnWindowWith(mod, "project") {
			t.Error("expected legacy agent window not to satisfy Spawn window lookup")
		}
	})
}

func TestIsPaneDeadWith(t *testing.T) {
	t.Parallel()
	t.Run("dead pane", func(t *testing.T) {
		mod := &tmuxtest.Fake{DeadPanes: map[string]bool{"%5": true}}
		if !isPaneDeadWith(mod, "%5") {
			t.Error("expected pane to report dead")
		}
	})

	t.Run("alive pane", func(t *testing.T) {
		mod := &tmuxtest.Fake{DeadPanes: map[string]bool{"%5": false}}
		if isPaneDeadWith(mod, "%5") {
			t.Error("expected pane to report alive")
		}
	})

	t.Run("unknown pane reports false", func(t *testing.T) {
		mod := &tmuxtest.Fake{}
		if isPaneDeadWith(mod, "%5") {
			t.Error("expected unknown pane to report alive")
		}
	})
}

func TestResolveSessionWith_WithProject(t *testing.T) {
	// Save and restore paneProject
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = "/home/user/my.project"

	mod := &tmuxtest.Fake{}
	withTmuxMod(t, mod)

	session, err := resolveSessionWith(mod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := project.SessionName(paneProject)
	if session != want {
		t.Errorf("got %q, want %q", session, want)
	}
	if mod.Live[want] != paneProject {
		t.Errorf("created sessions = %v, want %q -> %q", mod.Live, want, paneProject)
	}
}

func TestResolveSessionWith_ExistingSession(t *testing.T) {
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = "/home/user/project"

	want := project.SessionName(paneProject)
	mod := &tmuxtest.Fake{Live: map[string]string{want: paneProject}}
	withTmuxMod(t, mod)

	session, err := resolveSessionWith(mod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session != want {
		t.Errorf("got %q, want %q", session, want)
	}
}

func TestResolveSessionWith_NoProjectNotInTmux(t *testing.T) {
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = ""

	mod := &tmuxtest.Fake{CurrentSessionErr: fmt.Errorf("not in tmux")}

	_, err := resolveSessionWith(mod)
	if err == nil {
		t.Error("expected error when not in tmux and no --project")
	}
}

// newPaneInfoFake builds a stateful tmux fake that knows each pane's info,
// as auto-registration in runPaneSetStatusWith needs. paneInfo maps pane ID →
// "session\tpane_current_command"; unknown panes yield a PaneInfo error
// (matching tmux's behavior for non-existent panes). Panes are inactive by
// default (no dismiss downgrade).
func newPaneInfoFake(paneInfo map[string]string) *tmuxtest.Fake {
	infos := map[string]tmuxmod.PaneInfo{}
	for id, raw := range paneInfo {
		parts := strings.SplitN(raw, "\t", 2)
		if len(parts) == 2 {
			infos[id] = tmuxmod.PaneInfo{Session: parts[0], Command: parts[1]}
		}
	}
	return &tmuxtest.Fake{PaneInfos: infos}
}

func setupStateFile(t *testing.T, paneID string, status monitor.PaneStatus) string {
	t.Helper()
	dir := t.TempDir()
	setCmdLayerDeps(t, newTestCmdDeps(t, "", dir, ""))

	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	state := &monitor.State{
		Panes: map[string]*monitor.PaneEntry{
			paneID: {PaneID: paneID, Session: "test", Status: status},
		},
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(stateDir, "monitor.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(stateDir, "monitor.json")
}

func loadState(t *testing.T, path string) *monitor.State {
	t.Helper()
	state, err := monitor.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRunPaneCreateWith(t *testing.T) {
	// Save and restore paneProject (used by resolveSessionWith)
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = "/home/user/project"
	session := project.SessionName(paneProject)

	// titledPanes returns the fake's current titled panes for the target session.
	panes := func(mod *tmuxtest.Fake) []tmuxmod.TitledPane {
		got, _ := mod.WindowTitledPanes(session, spawnWindow)
		return got
	}

	t.Run("returns existing alive pane", func(t *testing.T) {
		mod := &tmuxtest.Fake{
			Windows: map[string]map[string][]string{
				session: {spawnWindow: {"%5"}},
			},
			PaneTitles: map[string]string{"%5": "mypane"},
		}
		withTmuxMod(t, mod)

		if err := runPaneCreateWith(mod, "mypane", "echo hi"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The alive pane is returned untouched: no new pane, no keys sent.
		if got := panes(mod); len(got) != 1 || got[0].ID != "%5" {
			t.Errorf("Spawn window panes = %v, want the single alive pane %%5", got)
		}
		if len(mod.SentKeys) != 0 {
			t.Errorf("expected no send-keys for an alive pane, got %v", mod.SentKeys)
		}
	})

	t.Run("kills dead pane and recreates with new-window", func(t *testing.T) {
		mod := &tmuxtest.Fake{
			Windows: map[string]map[string][]string{
				session: {spawnWindow: {"%5"}},
			},
			PaneTitles: map[string]string{"%5": "mypane"},
			DeadPanes:  map[string]bool{"%5": true},
		}
		withTmuxMod(t, mod)

		if err := runPaneCreateWith(mod, "mypane", "echo hi"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := panes(mod)
		if len(got) != 1 {
			t.Fatalf("Spawn window panes = %v, want exactly one fresh pane", got)
		}
		if got[0].ID == "%5" {
			t.Error("expected the dead pane %5 to be replaced by a fresh one")
		}
		if got[0].Title != "mypane" {
			t.Errorf("fresh pane title = %q, want mypane", got[0].Title)
		}
		if !mod.RemainOnExit[got[0].ID] {
			t.Error("expected remain-on-exit set on the fresh pane")
		}
		sent := mod.SentKeys[got[0].ID]
		if len(sent) != 1 || strings.Join(sent[0], "\x00") != strings.Join([]string{"echo hi", "Enter"}, "\x00") {
			t.Errorf("keys sent = %v, want the command then Enter", sent)
		}
	})

	t.Run("uses split-window when Spawn window exists", func(t *testing.T) {
		// A different pane already occupies the Spawn window, so mypane is
		// absent but the window exists → split path.
		mod := &tmuxtest.Fake{
			Windows: map[string]map[string][]string{
				session: {spawnWindow: {"%9"}},
			},
			PaneTitles: map[string]string{"%9": "other"},
		}
		withTmuxMod(t, mod)

		if err := runPaneCreateWith(mod, "mypane", "echo hi"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := panes(mod)
		if len(got) != 2 {
			t.Fatalf("Spawn window panes = %v, want the existing pane plus a split", got)
		}
		found := false
		for _, p := range got {
			if p.Title == "mypane" {
				found = true
			}
		}
		if !found {
			t.Error("expected a new pane titled mypane after the split")
		}
		wantRetile := session + ":" + spawnWindow
		if len(mod.WindowRetiled) == 0 || mod.WindowRetiled[len(mod.WindowRetiled)-1] != wantRetile {
			t.Errorf("expected the Spawn window to be re-tiled, retiled = %v", mod.WindowRetiled)
		}
	})

	t.Run("uses new-window when no Spawn window", func(t *testing.T) {
		mod := &tmuxtest.Fake{}
		withTmuxMod(t, mod)

		if err := runPaneCreateWith(mod, "mypane", "echo hi"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := panes(mod)
		if len(got) != 1 || got[0].Title != "mypane" {
			t.Errorf("Spawn window panes = %v, want a single new-window pane titled mypane", got)
		}
	})

	t.Run("creates pop-spawn beside legacy agent window", func(t *testing.T) {
		mod := &tmuxtest.Fake{
			Windows: map[string]map[string][]string{
				session: {"agent": {"%9"}},
			},
			PaneTitles: map[string]string{"%9": "legacy"},
		}
		withTmuxMod(t, mod)

		if err := runPaneCreateWith(mod, "mypane", "echo hi"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		legacy, _ := mod.WindowTitledPanes(session, "agent")
		if len(legacy) != 1 || legacy[0].ID != "%9" || legacy[0].Title != "legacy" {
			t.Errorf("legacy agent window = %v, want unchanged legacy pane %%9", legacy)
		}
		spawn, _ := mod.WindowTitledPanes(session, spawnWindow)
		if len(spawn) != 1 || spawn[0].Title != "mypane" {
			t.Errorf("Spawn window panes = %v, want single mypane pane", spawn)
		}
		if len(mod.KilledWindows) != 0 {
			t.Errorf("expected no windows killed, got %v", mod.KilledWindows)
		}
	})
}

// --- set-status dispatch ---

func startMonitorTestServer(t *testing.T, handler monitor.RequestHandler) string {
	t.Helper()
	ln, err := monitor.ListenAndServe("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func tcpServerEnabledCfg() *config.Config {
	return &config.Config{
		PaneMonitoring: &config.PaneMonitoringConfig{TCPServer: true},
	}
}

func TestRunPaneSetStatusWith_IgnoresConfiguredSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setCmdLayerDeps(t, newTestCmdDeps(t, "", dir, ""))

	cfg := &config.Config{
		PaneMonitoring: &config.PaneMonitoringConfig{
			IgnoreStatusFrom: []string{"tmux-global"},
			TCPServer:        true,
		},
	}

	tmuxCalled := false
	tmux := &tmuxtest.Fake{PaneInfoFunc: func(paneID string) (tmuxmod.PaneInfo, error) {
		tmuxCalled = true
		return tmuxmod.PaneInfo{}, nil
	}}

	if err := runPaneSetStatusWith(tmux, cfg, "tmux-global", false, "", []string{"%1", "working"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmuxCalled {
		t.Error("expected no tmux calls when source is ignored")
	}

	statePath := filepath.Join(dir, "pop", "monitor.json")
	if _, err := os.Stat(statePath); err == nil {
		state := loadState(t, statePath)
		if len(state.Panes) != 0 {
			t.Errorf("expected empty state, got %d panes", len(state.Panes))
		}
	}
}

// ADR-0145: POP_MONITOR_ADDR exercises real process env — stays serial.
func TestRunPaneSetStatusWith_SocketSuccessSkipsDirect(t *testing.T) {
	var handlerCalled bool
	addr := startMonitorTestServer(t, func(req monitor.Request) monitor.Response {
		handlerCalled = true
		return monitor.Response{OK: true}
	})
	t.Setenv("POP_MONITOR_ADDR", addr)

	dir := t.TempDir()
	setCmdLayerDeps(t, newTestCmdDeps(t, "", dir, ""))

	directWouldCallTmux := false
	tmux := &tmuxtest.Fake{PaneInfoFunc: func(paneID string) (tmuxmod.PaneInfo, error) {
		directWouldCallTmux = true
		return tmuxmod.PaneInfo{}, fmt.Errorf("direct path should not run")
	}}

	if err := runPaneSetStatusWith(tmux, tcpServerEnabledCfg(), "", false, "", []string{"%7", "working"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handlerCalled {
		t.Error("expected daemon handler to receive request")
	}
	if directWouldCallTmux {
		t.Error("expected socket success without direct fallback")
	}

	statePath := filepath.Join(dir, "pop", "monitor.json")
	if _, err := os.Stat(statePath); err == nil {
		state := loadState(t, statePath)
		if len(state.Panes) != 0 {
			t.Errorf("expected no local state write on socket path, got %d panes", len(state.Panes))
		}
	}
}

// ADR-0145: POP_MONITOR_ADDR exercises real process env — stays serial.
func TestRunPaneSetStatusWith_SocketFailureFallsBackAndStartsDaemon(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	t.Setenv("POP_MONITOR_ADDR", addr)

	dir := t.TempDir()
	setCmdLayerDeps(t, newTestCmdDeps(t, "", dir, ""))

	daemonStarted := make(chan struct{}, 1)
	oldHook := paneOnSocketSendFailed
	paneOnSocketSendFailed = func() { daemonStarted <- struct{}{} }
	t.Cleanup(func() { paneOnSocketSendFailed = oldHook })

	tmux := newPaneInfoFake(map[string]string{"%7": "sess\tcmd"})
	if err := runPaneSetStatusWith(tmux, tcpServerEnabledCfg(), "", false, "", []string{"%7", "working"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-daemonStarted:
	case <-time.After(time.Second):
		t.Fatal("expected daemon startup hook after socket failure")
	}

	state := loadState(t, filepath.Join(dir, "pop", "monitor.json"))
	entry, ok := state.Panes["%7"]
	if !ok {
		t.Fatal("expected direct fallback to register pane")
	}
	if entry.Status != monitor.StatusWorking {
		t.Errorf("status = %q, want %q", entry.Status, monitor.StatusWorking)
	}
}

// --- follow / unfollow ---

func TestResolvePaneArg(t *testing.T) {
	t.Parallel()
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = ""

	t.Run("returns pane_id verbatim when prefixed with %", func(t *testing.T) {
		mod := &tmuxtest.Fake{}
		got, err := resolvePaneArg(mod, "%42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "%42" {
			t.Errorf("got %q, want %%42", got)
		}
	})

	t.Run("resolves name via findPane in current session", func(t *testing.T) {
		mod := &tmuxtest.Fake{
			CurrentSessionName: "session-x",
			Windows: map[string]map[string][]string{
				"session-x": {spawnWindow: {"%5", "%6"}},
			},
			PaneTitles: map[string]string{"%5": "myagent", "%6": "other"},
		}
		got, err := resolvePaneArg(mod, "myagent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "%5" {
			t.Errorf("got %q, want %%5", got)
		}
	})
}
