package setkind

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// containerForRow builds the single container one refreshed row produces.
func containerForRow(t *testing.T, row tasks.Row) work.Container {
	t.Helper()
	rows := []tasks.Row{row}
	d := testDeps(t, rows)
	d.Refresh = func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{Rows: rows}, nil
	}
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}
	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("containers = %+v, want one", got)
	}
	return got[0]
}

// TestTheContainerCarriesTheExploreMarkTheRefreshResolved pins the dashboard's
// half of the single resolution (ADR-0262): the mark reaches the row's STATUS
// cell and the detail view as the refresh resolved it, the detail section
// carries the report as a pointer, and a set with no mark and no report
// authors no section at all.
func TestTheContainerCarriesTheExploreMarkTheRefreshResolved(t *testing.T) {
	explored := containerForRow(t, tasks.Row{ID: "demo", Status: tasks.StatusReady, Explore: tasks.ExploreResolution{
		Mark:      tasks.ExploreMarkExplored,
		HasReport: true,
		Pointer:   tasks.ExplorationPointer{Label: "Exploration", Path: "/w/demo/exploration.md", WorkSHA: "abc123a"},
	}})

	if explored.ExploreMark != tasks.ExploreMarkExplored {
		t.Fatalf("container mark = %q, want %q", explored.ExploreMark, tasks.ExploreMarkExplored)
	}
	if cell := tasks.WorkRowStatusCell(explored); !strings.Contains(cell, string(tasks.ExploreMarkExplored)) {
		t.Fatalf("STATUS cell = %q, want the explored mark", cell)
	}
	if len(explored.DetailSections) == 0 || explored.DetailSections[0].Title != tasks.ExplorationSectionTitle {
		t.Fatalf("detail sections = %+v, want the Exploration block first", explored.DetailSections)
	}
	body := explored.DetailSections[0].Body
	for _, want := range []string{"Explored", "/w/demo/exploration.md", "abc123a"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Exploration section missing %q:\n%s", want, body)
		}
	}

	quiet := containerForRow(t, tasks.Row{ID: "demo", Status: tasks.StatusReady})
	if quiet.ExploreMark != tasks.ExploreMarkNone {
		t.Fatalf("container mark = %q, want none", quiet.ExploreMark)
	}
	for _, section := range quiet.DetailSections {
		if section.Title == tasks.ExplorationSectionTitle {
			t.Fatalf("a set that never asked authored %+v", section)
		}
	}
}
