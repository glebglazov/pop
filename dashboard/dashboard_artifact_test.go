package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// artifactDetailKind pins the dashboard to the optional ArtifactSource seam. It
// deliberately uses the Task-set kind id without the Task-set implementation so
// these tests prove that the dashboard asks the seam instead of branching on a
// concrete kind.
type artifactDetailKind struct {
	id        work.KindID
	artifacts []work.Artifact
}

func (k *artifactDetailKind) ID() work.KindID                 { return k.id }
func (k *artifactDetailKind) Load() ([]work.Container, error) { return nil, nil }
func (k *artifactDetailKind) Less(a, b work.Container) bool   { return a.ID < b.ID }
func (k *artifactDetailKind) StatusCell(work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: "READY", Tone: work.ToneLabel}}
}
func (k *artifactDetailKind) Actions(work.Container) []work.Action       { return nil }
func (k *artifactDetailKind) StatusActions(work.Container) []work.Action { return nil }
func (k *artifactDetailKind) CopyActions(work.Container) []work.Action   { return nil }
func (k *artifactDetailKind) TypeWords() []string                        { return nil }
func (k *artifactDetailKind) ItemActions(work.Container, work.Item) []work.Action {
	return nil
}
func (k *artifactDetailKind) Perform(work.Container, *work.Item, work.Verb) (work.Outcome, error) {
	return work.Outcome{}, work.UnknownVerb(k.ID(), "item")
}
func (k *artifactDetailKind) Summary([]work.Container) []string { return nil }
func (k *artifactDetailKind) Columns() []string                 { return nil }

func (k *artifactDetailKind) Artifacts(work.Container) ([]work.Artifact, error) {
	return append([]work.Artifact(nil), k.artifacts...), nil
}

func (k *artifactDetailKind) ArtifactActions(work.Container, work.Artifact) []work.Action {
	return []work.Action{
		{Verb: work.VerbCopyName, Key: "y", Label: "copy name"},
		{Verb: setkind.VerbCopyPath, Key: "p", Label: "copy path"},
	}
}

func (k *artifactDetailKind) PerformArtifact(c work.Container, artifact work.Artifact, verb work.Verb) (work.Outcome, error) {
	payload := artifact.Path
	if verb == work.VerbCopyName {
		payload = filepath.ToSlash(strings.TrimPrefix(artifact.Path, filepath.Join(c.DefPath, c.ID)+string(filepath.Separator)))
	} else if verb != setkind.VerbCopyPath {
		return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
	}
	return work.Outcome{Kind: work.OutcomeMessage, Clipboard: payload, Message: "copied " + payload}, nil
}

type noArtifactMapKind struct{ *itemVerbKind }

func (*noArtifactMapKind) ID() work.KindID { return ref.KindMap }

func artifactDetailDashboard(kind work.Kind, row DashboardRow) QueueDashboard {
	d := &drain.Deps{Kinds: func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{kind} }}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 18
	return m
}

func openArtifactDetail(t *testing.T, m QueueDashboard) QueueDashboard {
	t.Helper()
	updated, cmd := m.update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if cmd != nil {
		t.Fatal("opening a detail should use the container and artifact seam in hand")
	}
	return updated.(QueueDashboard)
}

func TestArtifactViewSwitchesListsAndKeepsDetailSections(t *testing.T) {
	root := t.TempDir()
	row := genericDetailRow()
	row.DefPath = root
	newest := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	older := newest.Add(-24 * time.Hour)
	kind := &artifactDetailKind{id: ref.KindTaskSet, artifacts: []work.Artifact{
		{Type: "refine", Name: "refine-20260817T120000Z.md", Path: filepath.Join(root, row.ID, "refine", "refine-20260817T120000Z.md"), At: newest},
		{Type: "spec", Name: "spec.md", Path: filepath.Join(root, row.ID, "spec.md"), At: older},
	}}
	m := openArtifactDetail(t, artifactDetailDashboard(kind, row))

	items := m.View().Content
	if !strings.Contains(items, "STATUS") || !strings.Contains(items, "First") || !strings.Contains(items, "v artifacts") {
		t.Fatalf("item detail does not advertise its available Artifact view:\n%s", items)
	}

	updated, _ := m.update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = updated.(QueueDashboard)
	artifacts := m.View().Content
	for _, want := range []string{"Destination", "Somewhere worth going", "TYPE", "WRITTEN", "FILENAME", "refine", newest.Format(time.RFC3339), "refine-20260817T120000Z.md", "spec.md", "v tasks"} {
		if !strings.Contains(artifacts, want) {
			t.Fatalf("Artifact view missing %q:\n%s", want, artifacts)
		}
	}
	if refineAt, specAt := strings.Index(artifacts, "refine-20260817T120000Z.md"), strings.Index(artifacts, "spec.md"); refineAt < 0 || refineAt > specAt {
		t.Fatalf("artifacts are not in the order the kind published:\n%s", artifacts)
	}
	if strings.Contains(artifacts, "First") {
		t.Fatalf("Artifact view still renders the task list:\n%s", artifacts)
	}

	updated, _ = m.update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	items = updated.(QueueDashboard).View().Content
	if !strings.Contains(items, "STATUS") || !strings.Contains(items, "First") {
		t.Fatalf("second v did not return to tasks:\n%s", items)
	}
}

func TestDetailWithNoArtifactsNeverAdvertisesOrEntersArtifactView(t *testing.T) {
	tests := []struct {
		name string
		kind work.Kind
		row  DashboardRow
	}{
		{
			name: "empty Task set artifact source",
			kind: &artifactDetailKind{id: ref.KindTaskSet},
			row:  genericDetailRow(),
		},
		{
			name: "Map with no artifact seam",
			kind: &noArtifactMapKind{itemVerbKind: &itemVerbKind{}},
			row:  func() DashboardRow { r := genericDetailRow(); r.Kind = ref.KindMap; return r }(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := openArtifactDetail(t, artifactDetailDashboard(tc.kind, tc.row))
			before := m.View().Content
			if strings.Contains(before, "v artifacts") {
				t.Fatalf("detail footer advertises an empty Artifact view:\n%s", before)
			}
			for _, entry := range m.helpEntries() {
				if entry.Key == "v" {
					t.Fatalf("detail help advertises an empty Artifact view: %+v", m.helpEntries())
				}
			}
			updated, cmd := m.update(tea.KeyPressMsg{Code: 'v', Text: "v"})
			got := updated.(QueueDashboard)
			if cmd != nil || got.detail.artifacts || got.View().Content != before {
				t.Fatalf("v changed a detail with no artifacts:\n%s", got.View().Content)
			}
		})
	}
}

func TestArtifactPeekAndCopyVerbsStayOnTheirInvokingSurface(t *testing.T) {
	root := t.TempDir()
	row := genericDetailRow()
	row.DefPath = root
	path := filepath.Join(root, row.ID, "refine", "refine-20260817T120000Z.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&body, "- line %02d\n", i)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	kind := &artifactDetailKind{id: ref.KindTaskSet, artifacts: []work.Artifact{{
		Type: "refine", Name: filepath.Base(path), Path: path, At: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
	}}}
	m := openArtifactDetail(t, artifactDetailDashboard(kind, row))
	updated, _ := m.update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = updated.(QueueDashboard)
	var copied string
	m.copyFunc = func(payload string) error { copied = payload; return nil }

	run := func(t *testing.T, m QueueDashboard, key tea.KeyPressMsg) QueueDashboard {
		t.Helper()
		updated, cmd := m.update(key)
		m = updated.(QueueDashboard)
		if cmd == nil {
			t.Fatalf("%s did not dispatch", key.String())
		}
		updated, _ = m.update(cmd())
		return updated.(QueueDashboard)
	}

	// Both artifact verbs run from the row itself.
	m = run(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if copied != "refine/"+filepath.Base(path) || !strings.Contains(m.detail.flash.Text(), copied) {
		t.Fatalf("row y copied %q with flash %q", copied, m.detail.flash.Text())
	}
	m = run(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if copied != path || !strings.Contains(m.detail.flash.Text(), path) {
		t.Fatalf("row p copied %q with flash %q", copied, m.detail.flash.Text())
	}

	// Both entries in the row menu dispatch through the same artifact seam.
	for _, key := range []string{"y", "p"} {
		updated, _ = m.update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		m = updated.(QueueDashboard)
		if m.itemMenu == nil || m.itemMenu.artifact == nil {
			t.Fatal("artifact row did not open its own run menu")
		}
		m = run(t, m, tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	}

	// Enter opens the existing Document peek, and its existing movement scrolls it.
	updated, cmd := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(QueueDashboard)
	if cmd == nil || m.detail.peek == nil || m.detail.peek.artifactPath != path {
		t.Fatal("Enter did not open the artifact in the Document peek")
	}
	updated, _ = m.update(cmd())
	m = updated.(QueueDashboard)
	if !strings.Contains(m.detail.peek.text, "line 39") {
		t.Fatalf("peek did not read the artifact: %+v", m.detail.peek)
	}
	updated, _ = m.update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	if m.detail.peek.scroll != 1 {
		t.Fatalf("artifact peek scroll = %d, want 1", m.detail.peek.scroll)
	}

	// Direct copy and the peek's menu both report on the peek, not the detail.
	for _, key := range []string{"y", "p"} {
		m.detail.flash.Set("")
		m = run(t, m, tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		if m.detail.peek.flash.Text() == "" || m.detail.flash.Text() != "" {
			t.Fatalf("peek %s flash landed on the wrong surface: peek=%q detail=%q", key, m.detail.peek.flash.Text(), m.detail.flash.Text())
		}
	}
	for _, key := range []string{"y", "p"} {
		updated, _ = m.update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		m = updated.(QueueDashboard)
		if m.itemMenu == nil || m.itemMenu.artifact == nil || !m.itemMenu.inPeek {
			t.Fatal("artifact peek did not open its artifact run menu")
		}
		m = run(t, m, tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		if m.detail.peek.flash.Text() == "" {
			t.Fatalf("peek menu %s did not report on the peek", key)
		}
	}
}
