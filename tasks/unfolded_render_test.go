package tasks

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

func TestStatusColumnTextUnfolded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		row  Row
		want string
	}{
		{"bound DONE", Row{Status: StatusDone, Bound: true}, "DONE · unfolded"},
		{"bound AWAITING-APPROVAL", Row{Status: StatusAwaitingApproval, Bound: true}, "AWAITING-APPROVAL · unfolded"},
		{"unbound DONE", Row{Status: StatusDone}, "DONE"},
		{"bound READY", Row{Status: StatusReady, Bound: true}, "READY"},
		{"started READY", Row{Status: StatusReady, Started: true, Bound: true}, "IN PROGRESS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusColumnText(tc.row); got != tc.want {
				t.Fatalf("statusColumnText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAttachBound(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	d := &Deps{FS: deps.NewRealFileSystem()}
	s, ok, err := d.Store(true)
	if err != nil || !ok {
		t.Fatalf("Store: ok=%v err=%v", ok, err)
	}
	if err := s.PutBinding(store.Binding{
		ScopedKey:   "repo\x00bound-done",
		RuntimePath: "/wt/bound-done",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutBinding(store.Binding{
		ScopedKey:   "repo\x00blank",
		RuntimePath: "  ",
	}); err != nil {
		t.Fatal(err)
	}
	rows := []Row{
		{ID: "bound-done", Status: StatusDone},
		{ID: "blank", Status: StatusDone},
		{ID: "unbound", Status: StatusDone},
	}
	AttachBound(d, rows)
	if !rows[0].Bound {
		t.Fatal("bound-done should be Bound")
	}
	if rows[1].Bound {
		t.Fatal("blank runtime path should not be Bound")
	}
	if rows[2].Bound {
		t.Fatal("unbound should not be Bound")
	}
	table := formatTable(rows)
	if !strings.Contains(table, "DONE · unfolded") {
		t.Fatalf("table missing unfolded mark:\n%s", table)
	}
	if strings.Count(table, "unfolded") != 1 {
		t.Fatalf("want exactly one unfolded mark:\n%s", table)
	}
}
