package tasks

import (
	"path/filepath"
	"testing"
)

// withDerivationStamp swaps the process stamp for a fixed one, which is how a
// test moves the validating build without rebuilding a binary. Mirrors
// withManifestMemo, and carries its constraint: the stamp is process-wide by
// design, so tests using it must not run in parallel.
func withDerivationStamp(t *testing.T, stamp string) {
	t.Helper()
	previous := derivationStamp
	derivationStamp = func() string { return stamp }
	t.Cleanup(func() { derivationStamp = previous })
}

// The stamp's whole contract over one untouched set folder: the build that
// derived a verdict is served it back, and a different build is not — with not a
// byte and not an mtime moving between the two.
func TestManifestCacheServesAVerdictOnlyToTheBuildThatDerivedIt(t *testing.T) {
	withManifestMemo(t, 8)
	withDerivationStamp(t, "build-one")
	root := t.TempDir()
	setDir := filepath.Join(root, "demo")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, counting := countingDeps(t, root)
	manifestPath := filepath.Join(setDir, ManifestFileName)

	if m := LoadManifest(d, "demo", manifestPath); !m.Valid {
		t.Fatalf("first load invalid: %v", m.Errors)
	}
	reads := counting.mdReads

	// Same build, same folder: the warm path still answers, so the stamp has not
	// quietly disabled the cache it keys.
	same := laterProcess(t, d)
	if m := LoadManifest(same, "demo", manifestPath); !m.Valid {
		t.Fatalf("load under the same stamp invalid: %v", m.Errors)
	}
	if counting.mdReads != reads {
		t.Fatalf("markdown reads under the same stamp = %d, want the persisted tier to serve it (%d)", counting.mdReads, reads)
	}

	// A different build over the same bytes: its rules never ran here, so the
	// verdict is derived again rather than inherited.
	withDerivationStamp(t, "build-two")
	upgraded := laterProcess(t, same)
	if m := LoadManifest(upgraded, "demo", manifestPath); !m.Valid {
		t.Fatalf("load under the moved stamp invalid: %v", m.Errors)
	}
	if counting.mdReads <= reads {
		t.Fatalf("markdown reads under the moved stamp = %d, want the set re-validated (>%d)", counting.mdReads, reads)
	}
	if rows, _ := persistedRows(t, upgraded, setDir); rows != 1 {
		t.Fatalf("rows after the build moved = %d, want the one row overwritten in place (1)", rows)
	}
}

// A process that cannot fingerprint its own executable shares a key with
// nothing: it is served no other build's rows, and its own are served to no
// later process.
func TestUnresolvedDerivationStampSharesNoRowWithAnyBuild(t *testing.T) {
	withManifestMemo(t, 8)
	if first, second := unresolvedDerivationStamp(), unresolvedDerivationStamp(); first == second {
		t.Fatalf("two unresolvable processes minted the same stamp (%q); every build would share it", first)
	}
	root := t.TempDir()
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, counting := countingDeps(t, root)
	manifestPath := filepath.Join(root, "demo", ManifestFileName)

	withDerivationStamp(t, "build-one")
	if m := LoadManifest(d, "demo", manifestPath); !m.Valid {
		t.Fatalf("first load invalid: %v", m.Errors)
	}
	reads := counting.mdReads

	withDerivationStamp(t, unresolvedDerivationStamp())
	unresolved := laterProcess(t, d)
	if m := LoadManifest(unresolved, "demo", manifestPath); !m.Valid {
		t.Fatalf("load under an unresolvable stamp invalid: %v", m.Errors)
	}
	if counting.mdReads <= reads {
		t.Fatalf("markdown reads under an unresolvable stamp = %d, want a miss (>%d)", counting.mdReads, reads)
	}
	reads = counting.mdReads

	withDerivationStamp(t, unresolvedDerivationStamp())
	if m := LoadManifest(laterProcess(t, unresolved), "demo", manifestPath); !m.Valid {
		t.Fatalf("load under a second unresolvable stamp invalid: %v", m.Errors)
	}
	if counting.mdReads <= reads {
		t.Fatalf("markdown reads under a second unresolvable stamp = %d, want the first one's row unserved (>%d)", counting.mdReads, reads)
	}
}
