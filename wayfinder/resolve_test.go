package wayfinder

import (
	"slices"
	"strings"
	"testing"
)

// TestCompletionMapIDsOrdersNewestFirst pins the map-id completion helpers to
// ADR-0215: both the visible and archived-only variants offer the newest Map
// first, and the ambiguous-identifier prose list does the same.
func TestCompletionMapIDsOrdersNewestFirst(t *testing.T) {
	files := oneTicketMap("2026-08-03-demo")
	for rel, content := range oneTicketMap("2026-08-05-later") {
		files[rel] = content
	}
	for rel, content := range oneTicketMap("2026-08-01-earliest") {
		files[rel] = content
	}
	d, _ := registryFixture(t, files)
	mustRegister(t, d, "2026-08-03-demo")
	mustRegister(t, d, "2026-08-05-later")
	mustRegister(t, d, "2026-08-01-earliest")
	if _, err := ArchiveMap(d, "", "2026-08-01-earliest"); err != nil {
		t.Fatal(err)
	}

	want := []string{"2026-08-05-later", "2026-08-03-demo"}
	if got := CompletionMapIDs(d, "", false); !slices.Equal(got, want) {
		t.Fatalf("CompletionMapIDs(visible) = %v, want %v", got, want)
	}
	if got := CompletionMapIDs(d, "", true); !slices.Equal(got, []string{"2026-08-01-earliest"}) {
		t.Fatalf("CompletionMapIDs(archivedOnly) = %v, want [2026-08-01-earliest]", got)
	}

	_, err := FindMap(d, "", "not-a-real-map")
	if err == nil {
		t.Fatal("expected an error resolving an unknown map")
	}
	wantOrder := "2026-08-05-later, 2026-08-03-demo, 2026-08-01-earliest"
	if !strings.Contains(err.Error(), wantOrder) {
		t.Fatalf("error = %v, want the candidate list newest first (%s)", err, wantOrder)
	}
}
