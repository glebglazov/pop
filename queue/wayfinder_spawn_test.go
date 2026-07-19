package queue

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/wayfinder"
)

func wayfinderSpawnFixture(t *testing.T) (*Deps, *config.Config, DashboardRow, *recordingTmux, string) {
	t.Helper()
	storageDir := filepath.Join(t.TempDir(), "repos", "repo-wayfinder-spawn")
	activeMap := filepath.Join(storageDir, "wayfinder", "2026-07-01-active")
	files := map[string]string{
		filepath.Join(storageDir, "repo.json"):               `{"common_dir":"/repo/.git"}`,
		filepath.Join(activeMap, "map.md"):                 "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues/01-frontier.md"):  "Type: research\nStatus: open\n\n## Question\nA\n",
		filepath.Join(activeMap, "issues/02-blocked.md"):   "Type: research\nStatus: open\nBlocked by: 01\n\n## Question\nB\n",
		filepath.Join(activeMap, "issues/03-claimed.md"):   "Type: grilling\nStatus: claimed\n\n## Question\nC\n",
	}
	d := dashboardTestDeps(t, nil, nil)
	withWayfinderMaps(t, d, storageDir, files)
	repo := "/repo/checkout"
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := newRecordingTmux(false, "0")
	d.Project = project.DefaultDeps()
	d.Tmux = rt
	row := DashboardRow{
		IsMap:   true,
		Project: "pop",
		SetRef: SetRef{
			SetID:       "2026-07-01-active",
			DefPath:     filepath.Join(storageDir, "repo.json"),
			ProjectPath: repo,
			ProjectName: "pop",
		},
		MapOpen:     2,
		MapFrontier: 1,
	}
	return d, cfg, row, rt, storageDir
}

func TestLaunchWayfinderSessionTargetsNextFrontier(t *testing.T) {
	d, cfg, row, rt, _ := wayfinderSpawnFixture(t)
	result, err := LaunchWayfinderSession(d, cfg, row, "")
	if err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if result.TicketID != "01" {
		t.Fatalf("TicketID = %q, want 01", result.TicketID)
	}
	cmd, ok := extractWayfinderSpawnCommand(rt)
	if !ok {
		t.Fatal("expected send-keys with wayfinder command")
	}
	if !strings.Contains(cmd, "/pop-wayfinder work 2026-07-01-active 01") {
		t.Fatalf("spawn command = %q, want work-mode invocation with map and ticket", cmd)
	}
	if !strings.HasPrefix(cmd, "claude ") {
		t.Fatalf("spawn command = %q, want default interactive claude binary", cmd)
	}
}

func TestLaunchWayfinderSessionTargetsExplicitTicket(t *testing.T) {
	d, cfg, row, rt, storageDir := wayfinderSpawnFixture(t)
	files := map[string]string{
		filepath.Join(storageDir, "repo.json"):               `{"common_dir":"/repo/.git"}`,
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "map.md"): "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: resolved\n\n## Question\nA\n",
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/02-blocked.md"):  "Type: research\nStatus: open\n\n## Question\nB\n",
	}
	withWayfinderMaps(t, d, storageDir, files)
	result, err := LaunchWayfinderSession(d, cfg, row, "02")
	if err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if result.TicketID != "02" {
		t.Fatalf("TicketID = %q, want 02", result.TicketID)
	}
	cmd, ok := extractWayfinderSpawnCommand(rt)
	if !ok || !strings.Contains(cmd, " 02") {
		t.Fatalf("spawn command = %q, want explicit ticket 02", cmd)
	}
}

func TestLaunchWayfinderSessionWindowNamedAfterMap(t *testing.T) {
	d, cfg, row, rt, _ := wayfinderSpawnFixture(t)
	if _, err := LaunchWayfinderSession(d, cfg, row, ""); err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	newWindow, ok := rt.findCommand("new-window")
	if !ok {
		t.Fatal("expected new-window for map-named window")
	}
	if !containsArg(newWindow, "-n", "2026-07-01-active") {
		t.Fatalf("new-window = %v, want -n 2026-07-01-active", newWindow)
	}
	for _, c := range rt.commands {
		if len(c) > 0 && c[0] == "new-window" && containsArg(c, "-n", drainWindowName) {
			t.Fatalf("must not create %q window, got %v", drainWindowName, c)
		}
	}
}

func TestLaunchWayfinderSessionCreatesDetachedSession(t *testing.T) {
	d, cfg, row, rt, _ := wayfinderSpawnFixture(t)
	if _, err := LaunchWayfinderSession(d, cfg, row, ""); err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	newSession, ok := rt.findCommand("new-session")
	if !ok {
		t.Fatal("expected detached session creation when absent")
	}
	if len(newSession) < 3 || newSession[2] != row.ProjectPath {
		t.Fatalf("new-session = %v, want cwd %q", newSession, row.ProjectPath)
	}
}

func TestLaunchWayfinderSessionEmptyFrontier(t *testing.T) {
	d, cfg, row, rt, storageDir := wayfinderSpawnFixture(t)
	files := map[string]string{
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: open\nBlocked by: 99\n\n## Question\nA\n",
	}
	withWayfinderMaps(t, d, storageDir, files)
	_, err := LaunchWayfinderSession(d, cfg, row, "")
	if !errors.Is(err, wayfinder.ErrEmptyFrontier) {
		t.Fatalf("err = %v, want ErrEmptyFrontier", err)
	}
	if _, ok := extractWayfinderSpawnCommand(rt); ok {
		t.Fatal("empty frontier must not spawn")
	}
}

func TestDashboardMapRowISpawnsNextFrontier(t *testing.T) {
	d, cfg, row, rt, _ := wayfinderSpawnFixture(t)
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("i on map row did not return a command")
	}
	msg := cmd()
	wfMsg, ok := msg.(dashboardWayfinderMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardWayfinderMsg", msg)
	}
	if wfMsg.err != nil {
		t.Fatalf("spawn err = %v", wfMsg.err)
	}
	if wfMsg.ticketID != "01" {
		t.Fatalf("ticketID = %q, want 01", wfMsg.ticketID)
	}
	updated, _ = updated.(QueueDashboard).Update(msg)
	got := updated.(QueueDashboard)
	if got.statusMsg == "" {
		t.Fatal("expected spawn status message")
	}
	if _, ok := extractWayfinderSpawnCommand(rt); !ok {
		t.Fatal("expected tmux spawn")
	}
}

func TestDashboardMapRowIEmptyFrontierMessage(t *testing.T) {
	d, cfg, row, _, storageDir := wayfinderSpawnFixture(t)
	files := map[string]string{
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: claimed\n\n## Question\nA\n",
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/02-blocked.md"):  "Type: research\nStatus: open\nBlocked by: 01\n\n## Question\nB\n",
	}
	withWayfinderMaps(t, d, storageDir, files)
	row.MapFrontier = 0
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("expected spawn command")
	}
	got := updated.(QueueDashboard)
	updated, _ = got.Update(cmd())
	got = updated.(QueueDashboard)
	if got.statusMsg != dashboardWayfinderEmptyFrontierMessage() {
		t.Fatalf("statusMsg = %q, want empty-frontier explanation", got.statusMsg)
	}
}

func extractWayfinderSpawnCommand(rt *recordingTmux) (string, bool) {
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		return "", false
	}
	for _, arg := range sendKeys {
		if strings.Contains(arg, "/pop-wayfinder work") || strings.Contains(arg, "/wayfinder work") {
			return arg, true
		}
	}
	joined := strings.Join(sendKeys, " ")
	if strings.Contains(joined, "wayfinder work") {
		return joined, true
	}
	return "", false
}

func containsArg(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
