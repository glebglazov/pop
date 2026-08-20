package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/confighost"
	"github.com/glebglazov/pop/conventions"
	"github.com/glebglazov/pop/ui"
)

// The Config dashboard is the one editor for everything in force in a directory
// (ADR-0212 decision 8): a repository's conventions are rows beside its config
// keys and preview exactly what `pop conventions get` prints. They are read-only
// — a convention is a document the convention verb writes, not a value this
// surface flips (ADR-0226). These tests drive the component over a real
// repository and real files, through the writer `pop config dashboard` builds.

type conventionDashboardFixture struct {
	repo    string
	home    string
	cfgPath string
	deps    *Deps
	writer  confighost.Writer
	// seeds is what pop put in front of the human on each editor open.
	seeds []string
}

func newConventionDashboardFixture(t *testing.T) *conventionDashboardFixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, repo)
	dataHome := filepath.Join(root, "xdg")
	cd := newTestCmdDeps(t, repo, dataHome, "")

	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("projects = [{ path = \"/main\" }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &conventionDashboardFixture{
		repo:    repo,
		home:    filepath.Join(dataHome, "home"),
		cfgPath: cfgPath,
		deps:    cd,
	}
	f.writer = confighost.NewWriter(cd.configDeps(), cfgPath, repo).WithConventions(cd.conventionsDeps())
	return f
}

func (f *conventionDashboardFixture) write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// twoLayers authors the two hand-written layers a repository usually has: the
// human's document for every repository, and the team's committed one.
func (f *conventionDashboardFixture) twoLayers(t *testing.T) {
	t.Helper()
	f.write(t, filepath.Join(f.home, ".agents", "docs", "commits.md"), "Imperative subjects.\n")
	f.write(t, filepath.Join(f.repo, "docs", "agents", "commits.md"), "Conventional commits, scope required.\n")
}

// filterConfigDashboard types a query the way a human does — every printable key
// goes to the search field, which is why the component needs no focus key.
func filterConfigDashboard(m *ui.ConfigDashboard, query string) {
	for _, r := range query {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func (f *conventionDashboardFixture) overlayPath() string {
	return filepath.Join(f.home, ".agents", "docs", "commits.overlay.md")
}

// dashboard opens the component exactly as runConfigDashboard does, with a
// scripted editor standing in for the human's $EDITOR.
func (f *conventionDashboardFixture) dashboard(t *testing.T, replies ...string) *ui.ConfigDashboard {
	t.Helper()
	rows, err := f.writer.Rows()
	if err != nil {
		t.Fatalf("Rows() error: %v", err)
	}
	editor := func(path string, done tea.ExecCallback) tea.Cmd {
		seed, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("scripted editor: %v", err)
		}
		f.seeds = append(f.seeds, string(seed))
		reply := ""
		if len(f.seeds) <= len(replies) {
			reply = replies[len(f.seeds)-1]
		}
		if err := os.WriteFile(path, []byte(reply), 0o644); err != nil {
			t.Errorf("scripted editor: %v", err)
		}
		return func() tea.Msg { return done(nil) }
	}
	m := ui.NewConfigDashboard(rows, ui.ConfigDashboardOpts{Writer: f.writer, Editor: editor})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// One list holds both sorts of row, and the search reaches a convention through
// the same field that finds a config key.
func TestConfigDashboardListsConventionsBesideConfigKeys(t *testing.T) {
	f := newConventionDashboardFixture(t)
	f.twoLayers(t)

	rows, err := f.writer.Rows()
	if err != nil {
		t.Fatalf("Rows() error: %v", err)
	}
	var commits ui.ConfigDashboardRow
	keys := map[string]bool{}
	for _, row := range rows {
		keys[row.Key] = true
		if row.Key == conventions.RowKey(conventions.KindCommits) {
			commits = row
		}
	}
	for _, want := range []string{"work.implement.agents", "conventions.commits", "conventions.issue-tracker", "conventions.code-review"} {
		if !keys[want] {
			t.Fatalf("no row for %s; the list holds %d rows", want, len(rows))
		}
	}
	if commits.Desc == "" {
		t.Error("the convention row carries no description to search or read")
	}
	// A kind resolves to one answer, so the team's document holding something the
	// human's document stood down is exactly a layer quietly losing — the state
	// that marker and that sort exist to report (ADR-0223).
	if !commits.Contested {
		t.Error("a document standing another down is not marked contested")
	}

	// One filter over one list: the query that finds a config key's path finds a
	// convention through the same field.
	m := f.dashboard(t)
	filterConfigDashboard(m, "commits")
	selectKey(t, m, "conventions.commits")
}

// Selecting a convention shows what is in force — the one answer and the
// overlay — and never a rank the answer stood down. It is rendered by the same
// package `pop conventions get` renders through, so the two cannot disagree.
func TestConfigDashboardPreviewsWhatIsInForce(t *testing.T) {
	f := newConventionDashboardFixture(t)
	f.twoLayers(t)

	m := f.dashboard(t)
	selectKey(t, m, "conventions.commits")
	row, _ := m.Selected()

	preview := row.Preview.Layers
	for _, want := range []string{"ANSWER: USER GLOBAL", "Imperative subjects.", "Provenance:"} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Contains(preview, "Conventional commits, scope required.") {
		t.Errorf("the preview shows a document the answer stood down:\n%s", preview)
	}

	// One resolution, one rendering: what the pane shows is inside what the
	// command prints for the same repository.
	var out bytes.Buffer
	if err := conventions.Get(f.deps.conventionsDeps(), &out, f.repo, conventions.KindCommits); err != nil {
		t.Fatalf("conventions get: %v", err)
	}
	answer := "----- ANSWER: USER GLOBAL (yours, every repository) -----"
	if !strings.Contains(out.String(), answer) || !strings.Contains(preview, answer) {
		t.Errorf("the pane and the command label the answer differently:\npane:\n%s\nget:\n%s", preview, out.String())
	}
	if !strings.Contains(ui.StripANSI(m.ViewContent()), "USER GLOBAL") {
		t.Errorf("the answer is nowhere on screen:\n%s", ui.StripANSI(m.ViewContent()))
	}
}

// The preview closes by naming both documents that are the human's to write, so
// a reader who wants to change the answer leaves with the paths and the verb.
func TestConfigDashboardPreviewNamesBothWritablePaths(t *testing.T) {
	f := newConventionDashboardFixture(t)
	f.twoLayers(t)

	m := f.dashboard(t)
	selectKey(t, m, "conventions.commits")
	row, _ := m.Selected()

	preview := row.Preview.Layers
	for _, want := range []string{
		f.overlayPath(),
		filepath.Join("projects"),
		"commits.md",
		"pop conventions set commits",
	} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview missing %q:\n%s", want, preview)
		}
	}
}

// A kind nobody has written an answer for previews pop's own, that rank being
// what is in force there.
func TestConfigDashboardPreviewsTheShippedAnswer(t *testing.T) {
	f := newConventionDashboardFixture(t)

	m := f.dashboard(t)
	selectKey(t, m, "conventions.commits")
	row, _ := m.Selected()

	if !strings.Contains(row.Preview.Layers, "ANSWER: SHIPPED") {
		t.Fatalf("a kind nobody answered does not preview pop's own answer:\n%s", row.Preview.Layers)
	}
}

// The three write keys do nothing on a convention row: no editor opens, no
// document is written or removed, and the dashboard reports no write for the
// host to re-read after.
func TestConfigDashboardLeavesConventionWritesToTheVerb(t *testing.T) {
	f := newConventionDashboardFixture(t)
	f.twoLayers(t)
	f.write(t, f.overlayPath(), "Never name pop in a subject line.\n")

	m := f.dashboard(t, "Something the human never gets to type.\n")
	selectKey(t, m, "conventions.commits")
	pressConfigDashboard(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	pressConfigDashboard(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	pressConfigDashboard(m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})

	if len(f.seeds) != 0 {
		t.Errorf("an editor opened on a convention row: %q", f.seeds)
	}
	if m.Wrote() {
		t.Error("the dashboard reports a write it never made")
	}
	body, err := os.ReadFile(f.overlayPath())
	if err != nil {
		t.Fatalf("the overlay did not survive the three keys: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "Never name pop in a subject line." {
		t.Fatalf("overlay = %q, want the human's own prose untouched", got)
	}
	view := ui.StripANSI(m.ViewContent())
	for _, gone := range []string{"enter edit", "C-y copy source", "C-x remove"} {
		if strings.Contains(view, gone) {
			t.Errorf("the footer offers %q on a convention row:\n%s", gone, view)
		}
	}
}

// The same keys still write a config key in the same list: read-only is a
// property of the row, not of the dashboard.
func TestConfigDashboardStillEditsAConfigKeyBesideConventions(t *testing.T) {
	f := newConventionDashboardFixture(t)
	f.twoLayers(t)

	m := f.dashboard(t, "work.implement.agents = [\"codex\"]\n")
	selectKey(t, m, "work.implement.agents")
	pressConfigDashboard(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(f.seeds) != 1 {
		t.Fatalf("editor opened %d times on a config key, want once", len(f.seeds))
	}
	if !m.Wrote() {
		t.Fatal("the dashboard reports no write for a config key")
	}
	row, _ := m.Selected()
	if !row.Overridden || !strings.Contains(row.Preview.ValueTOML, "codex") {
		t.Fatalf("row = %+v, want the override in force the moment the editor closed", row)
	}
	if !strings.Contains(ui.StripANSI(m.ViewContent()), "enter edit") {
		t.Errorf("the footer does not offer the keys on a config row:\n%s", ui.StripANSI(m.ViewContent()))
	}
}

// A convention is a repository's, so outside one there is nothing to offer —
// the rule the repository-scope config rows already follow, since the two picker
// hosts open wherever the human is standing.
func TestConfigDashboardWithoutARepositoryOffersNoConventionRows(t *testing.T) {
	f := newConventionDashboardFixture(t)
	elsewhere := t.TempDir()
	f.writer = confighost.NewWriter(f.deps.configDeps(), f.cfgPath, elsewhere).
		WithConventions(f.deps.conventionsDeps())

	rows, err := f.writer.Rows()
	if err != nil {
		t.Fatalf("Rows() error: %v", err)
	}
	for _, row := range rows {
		if _, ok := conventions.RowKind(row.Key); ok {
			t.Errorf("row %s offered outside a repository", row.Key)
		}
	}
}
