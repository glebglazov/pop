package integrate

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIssueTrackerDoc_EmbeddedContent asserts the embedded pop Work store doc
// carries the three per-operation sections and uses spec.md throughout — never
// the legacy prd.md filename (ADR-0136).
func TestIssueTrackerDoc_EmbeddedContent(t *testing.T) {
	t.Parallel()
	body := string(issueTrackerDoc)
	if len(body) == 0 {
		t.Fatal("embedded work store doc is empty")
	}
	for _, section := range []string{
		"## Publishing a spec",
		"## Publishing tickets",
		"## Wayfinding operations",
	} {
		if !strings.Contains(body, section) {
			t.Errorf("embedded doc missing section %q", section)
		}
	}
	if !strings.Contains(body, "spec.md") {
		t.Error("embedded doc must reference the spec.md artifact")
	}
	if strings.Contains(body, "prd.md") {
		t.Error("embedded doc must not use the legacy prd.md filename")
	}
	if strings.Contains(body, "XDG_CONFIG_HOME") {
		t.Error("embedded doc must not reference the legacy config-dir path")
	}
	if strings.Contains(body, "XDG_DATA_HOME") {
		t.Error("embedded doc must not quote a pop data-dir path (ADR-0169)")
	}
	if !strings.Contains(body, "~/.agents/docs/issue-tracker.md") {
		t.Error("embedded doc must name the vendor-neutral user-level path")
	}
}

// TestIssueTrackerDoc_RegistrationDefault pins ADR-0192: one registration
// default in every checkout, and auto-drain as a standing invariant stated in
// the semantics rather than as a keyword-table row. A keyword-table edit that
// re-scopes auto-drain to a keyword, or that reinstates the locality branch,
// fails here.
func TestIssueTrackerDoc_RegistrationDefault(t *testing.T) {
	t.Parallel()
	body := string(issueTrackerDoc)

	for _, required := range []string{
		"`--auto-drain` is on unless `no-drain` / `manual` turns it off",
		"No other\n  keyword affects it: `managed` / `isolated` explicitly keeps it.",
		"the flags are the same in every checkout",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("embedded doc must state the auto-drain invariant: missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"| `trunk` |",
		"--managed --auto-drain",
		"Beats detection",
		"trunk-less fallback",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("embedded doc must not restate the retired locality branch: found %q", forbidden)
		}
	}
}

func issueTrackerDataPath(home string) string {
	return filepath.Join(home, ".local", "share", "pop", "agents", "docs", "issue-tracker.md")
}

func staleWorkStoreDataPath(home string) string {
	return filepath.Join(home, ".local", "share", "pop", "work-store.md")
}

func workStoreLegacyConfigPath(home string) string {
	return filepath.Join(home, ".config", "pop", "work-store.md")
}

// TestRemoveLegacyWorkStoreDoc covers present / absent / unwritable removal of
// the pre-ADR-0150 config-dir Work store doc.
func TestRemoveLegacyWorkStoreDoc(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		path := workStoreLegacyConfigPath("/h")
		fs.files[path] = []byte("# legacy config-dir copy\n")

		outcome := removeLegacyWorkStoreDoc(fakeDeps("/h", fs, io.Discard))
		if outcome == nil {
			t.Fatal("expected removal outcome when legacy doc is present")
		}
		if outcome.Skill != path {
			t.Errorf("outcome path = %q, want %q", outcome.Skill, path)
		}
		if outcome.Label != "removed" {
			t.Errorf("outcome label = %q, want removed", outcome.Label)
		}
		if _, ok := fs.files[path]; ok {
			t.Errorf("legacy doc still present at %s", path)
		}
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		if outcome := removeLegacyWorkStoreDoc(fakeDeps("/h", fs, io.Discard)); outcome != nil {
			t.Errorf("expected no outcome when legacy doc is absent, got %+v", outcome)
		}
	})

	t.Run("unwritable", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		path := workStoreLegacyConfigPath("/h")
		fs.files[path] = []byte("# legacy config-dir copy\n")
		fs.removeErr[path] = os.ErrPermission

		if outcome := removeLegacyWorkStoreDoc(fakeDeps("/h", fs, io.Discard)); outcome != nil {
			t.Errorf("expected no outcome when removal fails, got %+v", outcome)
		}
		if _, ok := fs.files[path]; !ok {
			t.Error("legacy doc must remain when removal fails")
		}
	})
}

// TestRefresh_LegacyWorkStoreDocRemoval proves Integration refresh deletes the
// config-dir copy when present, stays silent when absent, and does not fail when
// removal is blocked.
func TestRefresh_LegacyWorkStoreDocRemoval(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		setupIntegrateConfigLayer(t)
		fs := newFakeFS()
		legacyPath := workStoreLegacyConfigPath("/h")
		fs.files[legacyPath] = []byte("# legacy config-dir copy\n")

		_, real := fakeFactories("/h", fs)
		var stdout strings.Builder
		if err := RunUpdateExistingWith("rev-legacy1", testConfigDeps(t), real, &stdout, io.Discard, false); err != nil {
			t.Fatalf("RunUpdateExistingWith: %v", err)
		}
		if _, ok := fs.files[legacyPath]; ok {
			t.Errorf("refresh did not delete legacy doc at %s", legacyPath)
		}
		got := stdout.String()
		if !strings.Contains(got, legacyPath) || !strings.Contains(got, "removed") {
			t.Errorf("expected removal outcome naming %s, got %q", legacyPath, got)
		}
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		setupIntegrateConfigLayer(t)
		fs := newFakeFS()
		legacyPath := workStoreLegacyConfigPath("/h")

		_, real := fakeFactories("/h", fs)
		result := updateStaleIntegrations(testConfigDeps(t), real)
		for _, o := range result.Outcomes {
			if o.Skill == legacyPath || strings.Contains(o.Label, "removed") && strings.Contains(o.Skill, "work-store.md") {
				t.Errorf("refresh must not emit a removal outcome when legacy doc is absent, got %+v", o)
			}
		}
	})

	t.Run("unwritable", func(t *testing.T) {
		t.Parallel()
		setupIntegrateConfigLayer(t)
		fs := newFakeFS()
		legacyPath := workStoreLegacyConfigPath("/h")
		fs.files[legacyPath] = []byte("# legacy config-dir copy\n")
		fs.removeErr[legacyPath] = os.ErrPermission

		_, real := fakeFactories("/h", fs)
		if warnings := ensureForRevisionWith("rev-legacy2", testConfigDeps(t), real); len(warnings) != 0 {
			t.Fatalf("refresh must not fail on removal error, got warnings: %v", warnings)
		}
		if _, ok := fs.files[legacyPath]; !ok {
			t.Error("legacy doc must remain when removal is blocked")
		}
		result := updateStaleIntegrations(testConfigDeps(t), real)
		for _, o := range result.Outcomes {
			if o.Skill == legacyPath {
				t.Errorf("expected no removal outcome when delete fails, got %+v", o)
			}
		}
	})
}

// TestSeedIssueTrackerDoc_WritesWhenAbsent covers the write-if-different semantics:
// an empty machine writes the embedded doc verbatim to the data-dir path.
func TestSeedIssueTrackerDoc_WritesWhenAbsent(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps("/h", fs, io.Discard)

	if err := seedIssueTrackerDoc(d); err != nil {
		t.Fatalf("seedIssueTrackerDoc: %v", err)
	}
	want := issueTrackerDataPath("/h")
	got, ok := fs.files[want]
	if !ok {
		t.Fatalf("expected doc at %s, files: %v", want, sortedKeys(fs.files))
	}
	if !bytes.Equal(got, issueTrackerDoc) {
		t.Error("written doc bytes differ from the embedded doc")
	}
}

// TestSeedIssueTrackerDoc_RewritesWhenDifferent covers the rewrite semantics: a
// stale on-disk copy is replaced with the embedded bytes on every seed call.
func TestSeedIssueTrackerDoc_RewritesWhenDifferent(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	path := issueTrackerDataPath("/h")
	stale := []byte("# stale copy\n")
	fs.files[path] = append([]byte{}, stale...)

	d := fakeDeps("/h", fs, io.Discard)
	for i := 0; i < 3; i++ {
		if err := seedIssueTrackerDoc(d); err != nil {
			t.Fatalf("seedIssueTrackerDoc (pass %d): %v", i, err)
		}
	}
	if !bytes.Equal(fs.files[path], issueTrackerDoc) {
		t.Errorf("stale doc was not rewritten to embedded bytes: %q", fs.files[path])
	}
}

// TestSeedIssueTrackerDoc_SkipsWriteWhenMatching proves a refresh with matching
// bytes performs no write — the file map entry is left untouched.
func TestSeedIssueTrackerDoc_SkipsWriteWhenMatching(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	path := issueTrackerDataPath("/h")
	// Pre-seed with embedded bytes; a write would be observable via a counter.
	fs.files[path] = append([]byte{}, issueTrackerDoc...)
	var writes int

	d := fakeDeps("/h", fs, io.Discard)
	d.writeFile = func(p string, data []byte, mode os.FileMode) error {
		writes++
		return fs.WriteFile(p, data, mode)
	}

	for i := 0; i < 3; i++ {
		if err := seedIssueTrackerDoc(d); err != nil {
			t.Fatalf("seedIssueTrackerDoc (pass %d): %v", i, err)
		}
	}
	if writes != 0 {
		t.Errorf("expected no writes when bytes already match, got %d", writes)
	}
}

// TestSeedIssueTrackerDoc_RespectsXDGDataHome resolves the doc under
// $XDG_DATA_HOME/pop, not the home fallback, when the env var is set.
func TestSeedIssueTrackerDoc_RespectsXDGDataHome(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps("/h", fs, io.Discard)
	d.getenv = func(key string) string {
		if key == "XDG_DATA_HOME" {
			return "/data"
		}
		return ""
	}
	d.dataDir = func() (string, error) { return filepath.Join("/data", "pop"), nil }

	if err := seedIssueTrackerDoc(d); err != nil {
		t.Fatalf("seedIssueTrackerDoc: %v", err)
	}
	want := filepath.Join("/data", "pop", "agents", "docs", "issue-tracker.md")
	if _, ok := fs.files[want]; !ok {
		t.Fatalf("expected doc at %s, files: %v", want, sortedKeys(fs.files))
	}
	if _, ok := fs.files[issueTrackerDataPath("/h")]; ok {
		t.Error("doc must not be written under the home fallback when XDG_DATA_HOME is set")
	}
}

// TestRefresh_WritesWorkStoreDocOnceAcrossAgents proves the Shipped asset rides
// Integration refresh, is agent-agnostic, and is written once regardless of how
// many agents are integrated.
func TestRefresh_WritesWorkStoreDocOnceAcrossAgents(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	installViaFake(t, fs, "/h", "pi")

	_, real := fakeFactories("/h", fs)
	if warnings := ensureForRevisionWith("rev-seed1", testConfigDeps(t), real); warnings != nil {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	path := issueTrackerDataPath("/h")
	got, ok := fs.files[path]
	if !ok {
		t.Fatalf("refresh did not write the Work store doc at %s", path)
	}
	if !bytes.Equal(got, issueTrackerDoc) {
		t.Error("written doc bytes differ from the embedded doc")
	}
}

// TestRefresh_RewritesStaleWorkStoreDoc proves refresh rewrites a stale on-disk
// copy to match the embedded Shipped asset.
func TestRefresh_RewritesStaleWorkStoreDoc(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	path := issueTrackerDataPath("/h")
	stale := []byte("# hand-edited stale copy\n\nconsult pop docs.\n")
	fs.files[path] = append([]byte{}, stale...)

	_, real := fakeFactories("/h", fs)
	if warnings := ensureForRevisionWith("rev-seed2", testConfigDeps(t), real); warnings != nil {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	if !bytes.Equal(fs.files[path], issueTrackerDoc) {
		t.Errorf("refresh did not rewrite stale Work store doc: %q", fs.files[path])
	}
}

// TestRefresh_SkipsWriteWhenWorkStoreDocMatches proves refresh leaves a
// byte-identical on-disk copy untouched.
func TestRefresh_SkipsWriteWhenWorkStoreDocMatches(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	path := issueTrackerDataPath("/h")
	fs.files[path] = append([]byte{}, issueTrackerDoc...)
	var workStoreWrites int

	_, baseReal := fakeFactories("/h", fs)
	real := func() *Deps {
		d := baseReal()
		origWrite := d.writeFile
		d.writeFile = func(p string, data []byte, mode os.FileMode) error {
			if p == path {
				workStoreWrites++
			}
			return origWrite(p, data, mode)
		}
		return d
	}

	if warnings := ensureForRevisionWith("rev-seed3", testConfigDeps(t), real); warnings != nil {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	if workStoreWrites != 0 {
		t.Errorf("refresh wrote issue-tracker.md when bytes already matched, got %d writes", workStoreWrites)
	}
}

func userIssueTrackerLinkPath(home string) string {
	return filepath.Join(home, ".agents", "docs", "issue-tracker.md")
}

// agentSymlinks returns the links a refresh created for agents, excluding the
// agent-agnostic user-level Issue tracker doc link (ADR-0169).
func agentSymlinks(fs *fakeFS, home string) map[string]string {
	out := map[string]string{}
	for link, target := range fs.symlinks {
		if link == userIssueTrackerLinkPath(home) {
			continue
		}
		out[link] = target
	}
	return out
}

// seedUserIssueTrackerLink pre-creates the user-level link so refresh tests
// focused on other concerns observe no link outcome.
func seedUserIssueTrackerLink(fs *fakeFS, home string) {
	fs.dirs[filepath.Join(home, ".agents", "docs")] = true
	fs.symlinks[userIssueTrackerLinkPath(home)] = issueTrackerDataPath(home)
}

// TestLinkUserIssueTrackerDoc_CreatesWhenAbsent proves the vendor-neutral
// user-level link is created, with one outcome, when nothing occupies it.
func TestLinkUserIssueTrackerDoc_CreatesWhenAbsent(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()

	outcome := linkUserIssueTrackerDoc(fakeDeps("/h", fs, io.Discard))
	link := userIssueTrackerLinkPath("/h")
	if outcome == nil {
		t.Fatal("expected an outcome when the link is created")
	}
	if outcome.Skill != link {
		t.Errorf("outcome path = %q, want %q", outcome.Skill, link)
	}
	if outcome.Label != "linked" {
		t.Errorf("outcome label = %q, want linked", outcome.Label)
	}
	if got := fs.symlinks[link]; got != issueTrackerDataPath("/h") {
		t.Errorf("link target = %q, want %q", got, issueTrackerDataPath("/h"))
	}
	if !fs.dirs[filepath.Join("/h", ".agents", "docs")] {
		t.Error("expected ~/.agents/docs to be created")
	}
}

// TestLinkUserIssueTrackerDoc_OccupiedPathsLeftAlone walks every occupancy case:
// pop never overwrites and never repairs what is already at the link path.
func TestLinkUserIssueTrackerDoc_OccupiedPathsLeftAlone(t *testing.T) {
	t.Parallel()
	link := userIssueTrackerLinkPath("/h")

	tests := []struct {
		name  string
		setup func(*fakeFS)
		check func(*testing.T, *fakeFS)
	}{
		{
			name:  "regular file override",
			setup: func(fs *fakeFS) { fs.files[link] = []byte("# my own doc\n") },
			check: func(t *testing.T, fs *fakeFS) {
				if string(fs.files[link]) != "# my own doc\n" {
					t.Errorf("user file was modified: %q", fs.files[link])
				}
				if _, ok := fs.symlinks[link]; ok {
					t.Error("pop must not replace a user file with a link")
				}
			},
		},
		{
			name:  "directory",
			setup: func(fs *fakeFS) { fs.dirs[link] = true },
			check: func(t *testing.T, fs *fakeFS) {
				if _, ok := fs.symlinks[link]; ok {
					t.Error("pop must not link over a directory")
				}
			},
		},
		{
			name:  "symlink elsewhere",
			setup: func(fs *fakeFS) { fs.symlinks[link] = "/elsewhere/issue-tracker.md" },
			check: func(t *testing.T, fs *fakeFS) {
				if got := fs.symlinks[link]; got != "/elsewhere/issue-tracker.md" {
					t.Errorf("foreign link was repointed to %q", got)
				}
			},
		},
		{
			name:  "symlink already at pop asset",
			setup: func(fs *fakeFS) { fs.symlinks[link] = issueTrackerDataPath("/h") },
			check: func(t *testing.T, fs *fakeFS) {
				if got := fs.symlinks[link]; got != issueTrackerDataPath("/h") {
					t.Errorf("link target = %q, want unchanged", got)
				}
			},
		},
		{
			// Dangling only until the same refresh seeds the target, so it is
			// treated as correct rather than recreated.
			name: "dangling symlink at pop asset",
			setup: func(fs *fakeFS) {
				fs.symlinks[link] = issueTrackerDataPath("/h")
				delete(fs.files, issueTrackerDataPath("/h"))
			},
			check: func(t *testing.T, fs *fakeFS) {
				if got := fs.symlinks[link]; got != issueTrackerDataPath("/h") {
					t.Errorf("dangling link was rewritten to %q", got)
				}
			},
		},
		{
			name:  "docs dir is a regular file",
			setup: func(fs *fakeFS) { fs.files[filepath.Join("/h", ".agents", "docs")] = []byte("not a dir") },
			check: func(t *testing.T, fs *fakeFS) {
				if _, ok := fs.symlinks[link]; ok {
					t.Error("pop must not link when ~/.agents/docs is a file")
				}
				if fs.dirs[filepath.Join("/h", ".agents", "docs")] {
					t.Error("pop must not create a directory over a user file")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := newFakeFS()
			tc.setup(fs)
			if outcome := linkUserIssueTrackerDoc(fakeDeps("/h", fs, io.Discard)); outcome != nil {
				t.Errorf("expected no outcome for an occupied path, got %+v", outcome)
			}
			tc.check(t, fs)
		})
	}
}

// TestLinkUserIssueTrackerDoc_FailuresAreNonFatal proves a read-only home still
// integrates: mkdir and symlink errors yield no outcome and no panic.
func TestLinkUserIssueTrackerDoc_FailuresAreNonFatal(t *testing.T) {
	t.Parallel()

	t.Run("mkdir fails", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		fs.mkdirErr[filepath.Join("/h", ".agents", "docs")] = os.ErrPermission

		if outcome := linkUserIssueTrackerDoc(fakeDeps("/h", fs, io.Discard)); outcome != nil {
			t.Errorf("expected no outcome when mkdir fails, got %+v", outcome)
		}
		if _, ok := fs.symlinks[userIssueTrackerLinkPath("/h")]; ok {
			t.Error("no link may be created when the directory could not be made")
		}
	})

	t.Run("symlink fails", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		d := fakeDeps("/h", fs, io.Discard)
		d.symlink = func(string, string) error { return os.ErrPermission }

		if outcome := linkUserIssueTrackerDoc(d); outcome != nil {
			t.Errorf("expected no outcome when symlink fails, got %+v", outcome)
		}
	})
}

// TestRefresh_LinksUserIssueTrackerDoc proves Integration refresh publishes the
// user-level link and names it in one outcome line, and stays silent on a rerun.
func TestRefresh_LinksUserIssueTrackerDoc(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	fs := newFakeFS()

	_, real := fakeFactories("/h", fs)
	var stdout strings.Builder
	if err := RunUpdateExistingWith("rev-link1", testConfigDeps(t), real, &stdout, io.Discard, false); err != nil {
		t.Fatalf("RunUpdateExistingWith: %v", err)
	}
	link := userIssueTrackerLinkPath("/h")
	if got := fs.symlinks[link]; got != issueTrackerDataPath("/h") {
		t.Fatalf("refresh did not link %s at the shipped asset, got %q", link, got)
	}
	if out := stdout.String(); !strings.Contains(out, link) || !strings.Contains(out, "linked") {
		t.Errorf("expected a linked outcome naming %s, got %q", link, out)
	}

	result := updateStaleIntegrations(testConfigDeps(t), real)
	for _, o := range result.Outcomes {
		if o.Skill == link {
			t.Errorf("second refresh must not re-report the link, got %+v", o)
		}
	}
}

// TestRemoveStaleDataDirWorkStoreDoc covers present / absent / unwritable
// removal of the pre-ADR-0169 data-dir Work store doc.
func TestRemoveStaleDataDirWorkStoreDoc(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		path := staleWorkStoreDataPath("/h")
		fs.files[path] = []byte("# stale data-dir copy\n")

		outcome := removeStaleDataDirWorkStoreDoc(fakeDeps("/h", fs, io.Discard))
		if outcome == nil {
			t.Fatal("expected removal outcome when stale doc is present")
		}
		if outcome.Skill != path {
			t.Errorf("outcome path = %q, want %q", outcome.Skill, path)
		}
		if outcome.Label != "removed" {
			t.Errorf("outcome label = %q, want removed", outcome.Label)
		}
		if _, ok := fs.files[path]; ok {
			t.Errorf("stale doc still present at %s", path)
		}
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		if outcome := removeStaleDataDirWorkStoreDoc(fakeDeps("/h", fs, io.Discard)); outcome != nil {
			t.Errorf("expected no outcome when stale doc is absent, got %+v", outcome)
		}
	})

	t.Run("unwritable", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		path := staleWorkStoreDataPath("/h")
		fs.files[path] = []byte("# stale data-dir copy\n")
		fs.removeErr[path] = os.ErrPermission

		if outcome := removeStaleDataDirWorkStoreDoc(fakeDeps("/h", fs, io.Discard)); outcome != nil {
			t.Errorf("expected no outcome when removal fails, got %+v", outcome)
		}
		if _, ok := fs.files[path]; !ok {
			t.Error("stale doc must remain when removal fails")
		}
	})
}

// TestRefresh_StaleDataDirWorkStoreDocRemoval proves Integration refresh deletes
// the pre-ADR-0169 data-dir copy while seeding the asset at its new path, and
// does not fail when removal is blocked.
func TestRefresh_StaleDataDirWorkStoreDocRemoval(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		setupIntegrateConfigLayer(t)
		fs := newFakeFS()
		stalePath := staleWorkStoreDataPath("/h")
		fs.files[stalePath] = []byte("# stale data-dir copy\n")

		_, real := fakeFactories("/h", fs)
		var stdout strings.Builder
		if err := RunUpdateExistingWith("rev-stale1", testConfigDeps(t), real, &stdout, io.Discard, false); err != nil {
			t.Fatalf("RunUpdateExistingWith: %v", err)
		}
		if _, ok := fs.files[stalePath]; ok {
			t.Errorf("refresh did not delete stale doc at %s", stalePath)
		}
		if _, ok := fs.files[issueTrackerDataPath("/h")]; !ok {
			t.Errorf("refresh must seed the asset at its new path, files: %v", sortedKeys(fs.files))
		}
		got := stdout.String()
		if !strings.Contains(got, stalePath) || !strings.Contains(got, "removed") {
			t.Errorf("expected removal outcome naming %s, got %q", stalePath, got)
		}
	})

	t.Run("unwritable", func(t *testing.T) {
		t.Parallel()
		setupIntegrateConfigLayer(t)
		fs := newFakeFS()
		stalePath := staleWorkStoreDataPath("/h")
		fs.files[stalePath] = []byte("# stale data-dir copy\n")
		fs.removeErr[stalePath] = os.ErrPermission

		_, real := fakeFactories("/h", fs)
		if warnings := ensureForRevisionWith("rev-stale2", testConfigDeps(t), real); len(warnings) != 0 {
			t.Fatalf("refresh must not fail on removal error, got warnings: %v", warnings)
		}
		if _, ok := fs.files[stalePath]; !ok {
			t.Error("stale doc must remain when removal is blocked")
		}
	})
}
