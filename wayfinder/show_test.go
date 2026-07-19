package wayfinder

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func TestRenderShowGroupsTickets(t *testing.T) {
	m := Map{
		ID:             "demo",
		Status:         MapActive,
		Destination:    "Ship it",
		DecisionsSoFar: "- [01-first](issues/01-first.md) — use Postgres",
		Tickets: []Ticket{
			{Number: 1, ID: "01", Slug: "first", Type: TicketResearch, Status: TicketResolved},
			{Number: 2, ID: "02", Slug: "second", Type: TicketTask, Status: TicketOpen, BlockedBy: []string{"01"}},
			{Number: 3, ID: "03", Slug: "third", Type: TicketGrilling, Status: TicketClaimed},
			{Number: 4, ID: "04", Slug: "fourth", Type: TicketTask, Status: TicketOpen, BlockedBy: []string{"03"}},
			{Number: 5, ID: "05", Slug: "fifth", Type: TicketResearch, Status: TicketOpen},
		},
	}
	var buf strings.Builder
	if err := RenderShow(&buf, m); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Map: demo",
		"Status: active",
		"Destination: Ship it",
		"Decisions so far:",
		"use Post",
		"Frontier:",
		"02-second  task  open",
		"05-fifth  research  open",
		"Blocked:",
		"04-fourth  task  open  (blocked by 03)",
		"Claimed:",
		"03-third  grilling  claimed",
		"Resolved:",
		"01-first  research  resolved",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestShowMapUnknownID(t *testing.T) {
	dataHome := "/data"
	commonDir := "/repo/.git"
	t.Setenv("XDG_DATA_HOME", dataHome)
	id, err := tasks.IdentityFromCommonDir(&tasks.Deps{FS: deps.NewRealFileSystem()}, commonDir)
	if err != nil {
		t.Fatal(err)
	}
	mapDir := filepath.Join(id.StorageDir, "wayfinder", "known-map")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"): "## Destination\nknown",
	}
	d := wayfinderTestDeps(t, dataHome, commonDir, files)

	err = ShowWith(d, &strings.Builder{}, "", "missing-map")
	if err == nil {
		t.Fatal("expected error for unknown map")
	}
	if !strings.Contains(err.Error(), "unknown wayfinder map") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "known-map") {
		t.Fatalf("error should list valid maps: %v", err)
	}
}
