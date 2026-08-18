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
// keys, they preview exactly what `pop conventions get` prints, and editing one
// writes the human's overlay. These tests drive the component over a real
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
// human's defaults for every repository, and the team's committed document.
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
	for _, want := range []string{"ANSWER: USER DEFAULTS", "Imperative subjects.", "Provenance:"} {
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
	answer := "----- ANSWER: USER DEFAULTS (yours, every repository) -----"
	if !strings.Contains(out.String(), answer) || !strings.Contains(preview, answer) {
		t.Errorf("the pane and the command label the answer differently:\npane:\n%s\nget:\n%s", preview, out.String())
	}
	if !strings.Contains(ui.StripANSI(m.ViewContent()), "USER DEFAULTS") {
		t.Errorf("the answer is nowhere on screen:\n%s", ui.StripANSI(m.ViewContent()))
	}
}

// Editing a convention opens in place and writes prose: the human's overlay,
// which every later read appends to whichever rank answered.
func TestConfigDashboardEditsAConventionAsProse(t *testing.T) {
	f := newConventionDashboardFixture(t)
	f.twoLayers(t)

	m := f.dashboard(t, "Never name pop in a subject line.\n")
	selectKey(t, m, "conventions.commits")
	pressConfigDashboard(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if !m.Wrote() {
		t.Fatal("the dashboard reports no write")
	}
	// The buffer opens on the layer being written, under pop's own note — which
	// comes back out, so nothing pop wrote lands in the human's document.
	if len(f.seeds) != 1 || !strings.Contains(f.seeds[0], "overlay") {
		t.Fatalf("editor seeds = %q, want one buffer naming the layer it edits", f.seeds)
	}
	body, err := os.ReadFile(f.overlayPath())
	if err != nil {
		t.Fatalf("read overlay: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "Never name pop in a subject line." {
		t.Fatalf("overlay = %q, want the prose alone", got)
	}

	// The read command and the dashboard are one resolution: what was just
	// written is the top layer of the stack `pop conventions get` prints.
	var out bytes.Buffer
	if err := conventions.Get(f.deps.conventionsDeps(), &out, f.repo, conventions.KindCommits); err != nil {
		t.Fatalf("conventions get: %v", err)
	}
	if !strings.Contains(out.String(), "Never name pop in a subject line.") {
		t.Fatalf("the overlay is not in the stack get prints:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Your overlay is appended") {
		t.Errorf("the overlay is not reported as appended to the answer:\n%s", out.String())
	}

	row, _ := m.Selected()
	if !row.Overridden || !strings.Contains(row.Preview.Layers, "Never name pop in a subject line.") {
		t.Fatalf("row = %+v, want the new layer visible the moment the editor closed", row)
	}

	// ctrl+x hands the kind back to the answer alone, exactly as it hands a key
	// back to the source it stood on.
	pressConfigDashboard(m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if _, err := os.Stat(f.overlayPath()); !os.IsNotExist(err) {
		t.Fatalf("the overlay survived its removal: %v", err)
	}
	if row, _ := m.Selected(); row.Overridden {
		t.Errorf("row = %+v, want no layer of the human's own left in force", row)
	}
}

// Copying the source down assumes an override standing over a value, so that
// copying it changes nothing. The overlay is appended to the answer instead, so
// copying the answer into it would state the same prose twice — the action
// refuses and writes nothing.
func TestConfigDashboardRefusesToCopyAConventionDown(t *testing.T) {
	f := newConventionDashboardFixture(t)
	f.twoLayers(t)

	m := f.dashboard(t)
	selectKey(t, m, "conventions.commits")
	pressConfigDashboard(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})

	view := ui.StripANSI(m.ViewContent())
	if !strings.Contains(view, "appended to the answer") {
		t.Fatalf("the refusal does not say why, in the view:\n%s", view)
	}
	if _, err := os.Stat(f.overlayPath()); !os.IsNotExist(err) {
		t.Error("a refused copy still wrote the overlay")
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
