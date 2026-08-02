package wayfinder

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/work/ref"
)

func TestRegisterMapWritesRegistryRowAndIsIdempotent(t *testing.T) {
	d, _ := registryFixture(t, oneTicketMap("2026-08-03-demo"))

	first, err := RegisterMap(d, "", "2026-08-03-demo")
	if err != nil {
		t.Fatalf("RegisterMap: %v", err)
	}
	if first.AlreadyRegistered {
		t.Fatal("first register reported an existing row")
	}

	s, err := openWorkRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.WorkContainersOfKind(ref.KindMap)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Ref.ContainerID != "2026-08-03-demo" || rows[0].Ref.Kind != ref.KindMap {
		t.Fatalf("registry rows = %+v, want one (map, 2026-08-03-demo)", rows)
	}
	if rows[0].Archived {
		t.Fatal("a freshly registered map must not be archived")
	}
	if rows[0].RegisteredAt.IsZero() {
		t.Fatal("registry row carries no registration time")
	}

	second, err := RegisterMap(d, "", "2026-08-03-demo")
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if !second.AlreadyRegistered {
		t.Fatal("re-register did not report the existing row")
	}
	again, err := s.WorkContainersOfKind(ref.KindMap)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || !again[0].RegisteredAt.Equal(rows[0].RegisteredAt) {
		t.Fatalf("re-register disturbed the row: %+v", again)
	}
}

// TestRegisterMapNamesEveryManifestProblem is the MALFORMED fix loop: one run
// hands back the whole fix list, and the run after the fix registers.
func TestRegisterMapNamesEveryManifestProblem(t *testing.T) {
	files := oneTicketMap("2026-08-03-broken")
	files["maps/2026-08-03-broken/index.json"] = `{"tickets":[` +
		`{"id":"01","file":"01-first.md","type":"grilling","status":"parked","blocked_by":["07"]}` +
		`],"spawned_sets":[]}`
	files["maps/2026-08-03-broken/issues/02-orphan.md"] = "## Question\nUnlisted\n"
	d, storageDir := registryFixture(t, files)

	_, err := RegisterMap(d, "", "2026-08-03-broken")
	if err == nil {
		t.Fatal("expected a malformed manifest to refuse registration")
	}
	var malformed *MapMalformedError
	if !errors.As(err, &malformed) {
		t.Fatalf("error = %T %v, want *MapMalformedError", err, err)
	}
	for _, want := range []string{`unknown status "parked"`, `unresolved blocker "07"`, "02-orphan.md: no manifest entry"} {
		if !containsAny(malformed.Problems, want) {
			t.Fatalf("problems %q missing %q", malformed.Problems, want)
		}
	}
	if !strings.Contains(err.Error(), "pop map register 2026-08-03-broken") {
		t.Fatalf("error does not name the re-run: %v", err)
	}
	s, err := openWorkRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := s.WorkContainersOfKind(ref.KindMap); err != nil || len(rows) != 0 {
		t.Fatalf("refused registration still wrote a row: %+v (%v)", rows, err)
	}

	fixed := `{"tickets":[` +
		`{"id":"01","file":"01-first.md","type":"grilling","status":"open","blocked_by":[]},` +
		`{"id":"02","file":"02-orphan.md","type":"research","status":"open","blocked_by":["01"]}` +
		`],"spawned_sets":[]}`
	manifestPath := filepath.Join(storageDir, "maps", "2026-08-03-broken", MapManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterMap(d, "", "2026-08-03-broken"); err != nil {
		t.Fatalf("register after fix: %v", err)
	}
}

func TestRegisterMapRefusesMapWithNothingToRegister(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "no manifest",
			files: map[string]string{
				"maps/2026-08-03-charting/map.md": "Status: active\n\n## Destination\nStill charting\n",
			},
			want: "index.json: missing",
		},
		{
			name: "manifest with no tickets",
			files: map[string]string{
				"maps/2026-08-03-empty/map.md":     "Status: active\n\n## Destination\nNothing yet\n",
				"maps/2026-08-03-empty/index.json": `{"tickets":[],"spawned_sets":[]}`,
			},
			want: "no Decision tickets",
		},
		{
			name: "unreadable map.md status",
			files: map[string]string{
				"maps/2026-08-03-odd/map.md": "Status: sideways\n\n## Destination\nWhat\n",
				"maps/2026-08-03-odd/index.json": `{"tickets":[` +
					`{"id":"01","file":"01-first.md","type":"grilling","status":"open","blocked_by":[]}` +
					`],"spawned_sets":[]}`,
				"maps/2026-08-03-odd/issues/01-first.md": "## Question\nWhy?\n",
			},
			want: "sideways",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := registryFixture(t, tt.files)
			var mapID string
			for rel := range tt.files {
				mapID = strings.Split(strings.TrimPrefix(rel, "maps/"), "/")[0]
				break
			}
			_, err := RegisterMap(d, "", mapID)
			if err == nil {
				t.Fatalf("expected %s to refuse registration", mapID)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func containsAny(problems []string, want string) bool {
	for _, p := range problems {
		if strings.Contains(p, want) {
			return true
		}
	}
	return false
}
