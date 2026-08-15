package ui

import (
	"strings"
	"testing"
)

// The contested rows of ADR-0212 decision 8: a key more than one layer states a
// value for opens at the top of the list carrying its own marker, so the
// dashboard answers "what did I customize, and what is quietly fighting it"
// without a second view.

// contestedSampleRows is the sample list with three of its keys contested — one
// of them last in the caller's order, so a sort that did nothing would show, and
// one of them also overridden, which is the common case.
func contestedSampleRows() []ConfigDashboardRow {
	rows := sampleConfigDashboardRows()
	rows[0].Contested = true // work.implement.agents, overridden as well
	rows[1].Contested = true // work.verify.agents
	rows[4].Contested = true // worktree.root
	return rows
}

// listedKeys reads the key paths off the rendered list in the order they appear,
// which is what a human actually meets.
func listedKeys(view string, rows []ConfigDashboardRow) []string {
	var order []string
	for _, line := range strings.Split(view, "\n") {
		// Only the left pane lists keys; the preview on the right names them too.
		line, _, _ = strings.Cut(line, "│")
		for _, row := range rows {
			if strings.Contains(line, row.Key+" ") || strings.HasSuffix(strings.TrimRight(line, " "), row.Key) {
				order = append(order, row.Key)
				break
			}
		}
	}
	return order
}

func TestConfigDashboardSortsContestedKeysFirst(t *testing.T) {
	rows := contestedSampleRows()
	m := newSizedConfigDashboard(rows, 100, 30)
	got := configDashboardView(m)

	// The three contested keys first; within each group the caller's order
	// stands, so nothing else moves.
	want := []string{
		"work.implement.agents", "work.verify.agents", "worktree.root",
		"work.routine.agents", "work.attended.agents",
	}
	if order := listedKeys(got, rows); !equalKeys(order, want) {
		t.Errorf("listed order = %v, want %v:\n%s", order, want, got)
	}
}

func TestConfigDashboardMarksContestedRows(t *testing.T) {
	rows := contestedSampleRows()
	m := newSizedConfigDashboard(rows, 100, 30)
	got := configDashboardView(m)

	for _, row := range rows {
		marked := strings.Contains(got, configContestedMarker+configOverrideMarker+" "+row.Key) ||
			strings.Contains(got, configContestedMarker+"  "+row.Key)
		if marked != row.Contested {
			t.Errorf("row %q contested-marked=%v, want %v:\n%s", row.Key, marked, row.Contested, got)
		}
		// The override marker keeps its own column, so a row that is both says
		// both.
		if overridden := strings.Contains(got, configOverrideMarker+" "+row.Key); overridden != row.Overridden {
			t.Errorf("row %q override-marked=%v, want %v:\n%s", row.Key, overridden, row.Overridden, got)
		}
	}
}

// TestConfigDashboardSearchReachesEveryKeyDespiteTheSort pins that the sort
// moves rows and hides none: the filter runs over the whole list, so a key that
// was sorted to the bottom is still one query away.
func TestConfigDashboardSearchReachesEveryKeyDespiteTheSort(t *testing.T) {
	rows := contestedSampleRows()
	for _, row := range rows {
		m := newSizedConfigDashboard(rows, 100, 30)
		typeConfigQuery(m, row.Key)
		got := configDashboardView(m)
		if !strings.Contains(got, row.Key) {
			t.Errorf("query %q reached no row:\n%s", row.Key, got)
		}
		if selected, ok := m.Selected(); !ok || selected.Key != row.Key {
			t.Errorf("query %q selected %+v, want %s", row.Key, selected, row.Key)
		}
	}
}

func equalKeys(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
