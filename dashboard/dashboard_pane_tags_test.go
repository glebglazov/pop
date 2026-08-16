package dashboard

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

func seedTaggedPane(rt *queuetest.RecordingTmux, paneID string, tag tmuxmod.PaneTag, setID string) {
	if rt.PaneTagValues == nil {
		rt.PaneTagValues = map[string]map[tmuxmod.PaneTag]string{}
	}
	if rt.PaneTagValues[paneID] == nil {
		rt.PaneTagValues[paneID] = map[tmuxmod.PaneTag]string{}
	}
	rt.PaneTagValues[paneID][tag] = setID
}

// TestActivityPaneTagsDistinct asserts drain, verify, fold, and assist each use
// their own pane tag so a lookup for one activity never returns another's pane.
func TestActivityPaneTagsDistinct(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "pane-tags", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "pane-tags-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.Bound = true
	row.RawStatus = tasks.StatusNeedsVerify
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "pane-tags", Project: "pop", Provisioned: false},
	})

	if _, err := drain.LaunchDrain(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchDrain: %v", err)
	}
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	if _, err := drain.LaunchVerify(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchVerify: %v", err)
	}
	row.RawStatus = tasks.StatusDone
	if _, err := drain.LaunchAssist(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchAssist: %v", err)
	}

	drainPane := ""
	verifyPane := ""
	assistPane := ""
	for paneID, tags := range rt.PaneTagValues {
		if tags[tmuxmod.TagSet] == setID {
			if drainPane != "" {
				t.Fatalf("multiple drain panes for %s: %s and %s", setID, drainPane, paneID)
			}
			drainPane = paneID
		}
		if tags[tmuxmod.TagVerify] == setID {
			if verifyPane != "" {
				t.Fatalf("multiple verify panes for %s: %s and %s", setID, verifyPane, paneID)
			}
			verifyPane = paneID
		}
		if tags[tmuxmod.TagAssist] == setID {
			if assistPane != "" {
				t.Fatalf("multiple assist panes for %s: %s and %s", setID, assistPane, paneID)
			}
			assistPane = paneID
		}
		if tags[tmuxmod.TagFold] == setID {
			t.Fatalf("fold tag must not be set until fold spawns a pane")
		}
	}
	if drainPane == "" || verifyPane == "" || assistPane == "" {
		t.Fatalf("missing tagged panes: drain=%q verify=%q assist=%q tags=%v", drainPane, verifyPane, assistPane, rt.PaneTagValues)
	}
	if drainPane == verifyPane || drainPane == assistPane || verifyPane == assistPane {
		t.Fatalf("activities must not share a pane: drain=%s verify=%s assist=%s", drainPane, verifyPane, assistPane)
	}

	if got, _ := d.Tmux.FindTaggedPane(project.SessionNameWith(d.Project, repo), tmuxmod.DrainWindow, tmuxmod.TagSet, setID); got != drainPane {
		t.Fatalf("TagSet lookup = %q, want drain pane %q", got, drainPane)
	}
	if got, _ := d.Tmux.FindTaggedPane(project.SessionNameWith(d.Project, repo), tmuxmod.DrainWindow, tmuxmod.TagVerify, setID); got != verifyPane {
		t.Fatalf("TagVerify lookup = %q, want verify pane %q", got, verifyPane)
	}
	if got, _ := d.Tmux.FindTaggedPane(project.SessionNameWith(d.Project, repo), tmuxmod.DrainWindow, tmuxmod.TagAssist, setID); got != assistPane {
		t.Fatalf("TagAssist lookup = %q, want assist pane %q", got, assistPane)
	}
	if got, _ := d.Tmux.FindTaggedPane(project.SessionNameWith(d.Project, repo), tmuxmod.DrainWindow, tmuxmod.TagFold, setID); got != "" {
		t.Fatalf("TagFold lookup = %q, want empty before fold spawns", got)
	}
}

// TestVerifyWhileDrainLiveSpawnsSecondPane asserts verify with a live drain for
// the same set opens a second pane and sends no keystrokes to the drain pane.
func TestVerifyWhileDrainLiveSpawnsSecondPane(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "verify-with-drain", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "verify-with-drain-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.Bound = true
	row.RawStatus = tasks.StatusNeedsVerify
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "verify-with-drain", Project: "pop", Provisioned: false},
	})

	drainResult, err := drain.LaunchDrain(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchDrain: %v", err)
	}
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	drainSendKeys := rt.CountCommand("send-keys")

	if _, err := drain.LaunchVerify(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchVerify: %v", err)
	}
	if rt.CountCommand("send-keys") != drainSendKeys+1 {
		t.Fatalf("verify must spawn a fresh pane (one new send-keys), commands=%v", rt.Commands)
	}
	for _, c := range rt.Commands {
		if len(c) >= 4 && c[0] == "send-keys" && c[2] == drainResult.PaneID && strings.Contains(c[3], "pop tasks verify") {
			t.Fatalf("verify must not send-keys to drain pane %s, commands=%v", drainResult.PaneID, rt.Commands)
		}
	}
	verifyPane := ""
	for paneID, tags := range rt.PaneTagValues {
		if tags[tmuxmod.TagVerify] == setID {
			verifyPane = paneID
		}
	}
	if verifyPane == "" || verifyPane == drainResult.PaneID {
		t.Fatalf("verify pane = %q, drain pane = %q; want distinct tagged panes", verifyPane, drainResult.PaneID)
	}
}

// TestHandoffPaneTitles asserts every handoff activity pane is titled with its
// set id and activity name.
func TestHandoffPaneTitles(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "pane-titles", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "pane-titles-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.Bound = true
	row.RawStatus = tasks.StatusNeedsVerify
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "pane-titles", Project: "pop", Provisioned: false},
	})

	drainResult, err := drain.LaunchDrain(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchDrain: %v", err)
	}
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	verifyResult, err := drain.LaunchVerify(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchVerify: %v", err)
	}
	row.RawStatus = tasks.StatusDone
	assistResult, err := drain.LaunchAssist(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchAssist: %v", err)
	}
	foldResult, err := drain.LaunchFold(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchFold: %v", err)
	}

	want := map[string]string{
		drainResult.PaneID:  drain.DrainPaneTitle(setID),
		verifyResult.PaneID: drain.VerifyPaneTitle(setID),
		assistResult.PaneID: drain.AssistPaneTitle(setID, tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg))),
		foldResult.PaneID:   drain.FoldPaneTitle(setID),
	}
	for paneID, wantTitle := range want {
		if got := rt.PaneTitles[paneID]; got != wantTitle {
			t.Fatalf("pane %s title = %q, want %q", paneID, got, wantTitle)
		}
	}
}

// TestShellVerbSpawnsNothingTagged asserts the runtime shell verb does not tag
// any pane — it is the operator's process, not a supervised activity.
func TestShellVerbSpawnsNothingTagged(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "shell-untagged", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, cmd := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd = got.update(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd == nil {
		t.Fatal("shell verb must return a command")
	}
	// The shell is the kind's verb: it resolves the directory and hands the
	// dashboard a handoff outcome, which the model then spawns into.
	verbMsg, ok := cmd().(dashboardKindVerbMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardKindVerbMsg", cmd())
	}
	got = updated.(QueueDashboard)
	_, cmd = got.update(verbMsg)
	if cmd == nil {
		t.Fatal("shell outcome must spawn the pane")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if handoff.err != nil || !handoff.quit {
		t.Fatalf("handoff = %+v, want quit without err", handoff)
	}
	if len(rt.PaneTagValues) != 0 {
		t.Fatalf("shell must not tag panes, got %v", rt.PaneTagValues)
	}
	for _, c := range rt.Commands {
		if len(c) > 0 && c[0] == "set-option" {
			t.Fatalf("shell must not set pane options, commands=%v", rt.Commands)
		}
	}
}

// TestShellVerbTwiceYieldsTwoPanes asserts every shell press spawns a fresh
// untagged pane rather than reusing one.
func TestShellVerbTwiceYieldsTwoPanes(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "shell-twice", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.Fake.Inside = true

	first, err := drain.LaunchShell(d, cfg, row)
	if err != nil {
		t.Fatalf("first drain.LaunchShell: %v", err)
	}
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	second, err := drain.LaunchShell(d, cfg, row)
	if err != nil {
		t.Fatalf("second drain.LaunchShell: %v", err)
	}
	if first.PaneID == "" || second.PaneID == "" {
		t.Fatalf("shell panes empty: first=%q second=%q", first.PaneID, second.PaneID)
	}
	if first.PaneID == second.PaneID {
		t.Fatalf("second shell reused pane %s; want a fresh pane", first.PaneID)
	}
	if len(rt.PaneTagValues) != 0 {
		t.Fatalf("shell panes must stay untagged, got %v", rt.PaneTagValues)
	}
}

// TestFoldPaneTitleNaming pins the fold pane title helper for the next slice.
func TestFoldPaneTitleNaming(t *testing.T) {
	if drain.FoldPaneTitle("demo-set") != "demo-set-fold" {
		t.Fatalf("drain.FoldPaneTitle = %q, want demo-set-fold", drain.FoldPaneTitle("demo-set"))
	}
}
