package integrate

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkStoreDoc_EmbeddedContent asserts the embedded pop Work store doc
// carries the three per-operation sections and uses spec.md throughout — never
// the legacy prd.md filename (ADR-0136).
func TestWorkStoreDoc_EmbeddedContent(t *testing.T) {
	t.Parallel()
	body := string(workStoreDoc)
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
	if !strings.Contains(body, "XDG_DATA_HOME") {
		t.Error("embedded doc must name the data-dir Shipped-asset path")
	}
}

func workStoreDataPath(home string) string {
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

// TestSeedWorkStoreDoc_WritesWhenAbsent covers the write-if-different semantics:
// an empty machine writes the embedded doc verbatim to the data-dir path.
func TestSeedWorkStoreDoc_WritesWhenAbsent(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps("/h", fs, io.Discard)

	if err := seedWorkStoreDoc(d); err != nil {
		t.Fatalf("seedWorkStoreDoc: %v", err)
	}
	want := workStoreDataPath("/h")
	got, ok := fs.files[want]
	if !ok {
		t.Fatalf("expected doc at %s, files: %v", want, sortedKeys(fs.files))
	}
	if !bytes.Equal(got, workStoreDoc) {
		t.Error("written doc bytes differ from the embedded doc")
	}
}

// TestSeedWorkStoreDoc_RewritesWhenDifferent covers the rewrite semantics: a
// stale on-disk copy is replaced with the embedded bytes on every seed call.
func TestSeedWorkStoreDoc_RewritesWhenDifferent(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	path := workStoreDataPath("/h")
	stale := []byte("# stale copy\n")
	fs.files[path] = append([]byte{}, stale...)

	d := fakeDeps("/h", fs, io.Discard)
	for i := 0; i < 3; i++ {
		if err := seedWorkStoreDoc(d); err != nil {
			t.Fatalf("seedWorkStoreDoc (pass %d): %v", i, err)
		}
	}
	if !bytes.Equal(fs.files[path], workStoreDoc) {
		t.Errorf("stale doc was not rewritten to embedded bytes: %q", fs.files[path])
	}
}

// TestSeedWorkStoreDoc_SkipsWriteWhenMatching proves a refresh with matching
// bytes performs no write — the file map entry is left untouched.
func TestSeedWorkStoreDoc_SkipsWriteWhenMatching(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	path := workStoreDataPath("/h")
	// Pre-seed with embedded bytes; a write would be observable via a counter.
	fs.files[path] = append([]byte{}, workStoreDoc...)
	var writes int

	d := fakeDeps("/h", fs, io.Discard)
	d.writeFile = func(p string, data []byte, mode os.FileMode) error {
		writes++
		return fs.WriteFile(p, data, mode)
	}

	for i := 0; i < 3; i++ {
		if err := seedWorkStoreDoc(d); err != nil {
			t.Fatalf("seedWorkStoreDoc (pass %d): %v", i, err)
		}
	}
	if writes != 0 {
		t.Errorf("expected no writes when bytes already match, got %d", writes)
	}
}

// TestSeedWorkStoreDoc_RespectsXDGDataHome resolves the doc under
// $XDG_DATA_HOME/pop, not the home fallback, when the env var is set.
func TestSeedWorkStoreDoc_RespectsXDGDataHome(t *testing.T) {
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

	if err := seedWorkStoreDoc(d); err != nil {
		t.Fatalf("seedWorkStoreDoc: %v", err)
	}
	want := filepath.Join("/data", "pop", "work-store.md")
	if _, ok := fs.files[want]; !ok {
		t.Fatalf("expected doc at %s, files: %v", want, sortedKeys(fs.files))
	}
	if _, ok := fs.files[workStoreDataPath("/h")]; ok {
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

	path := workStoreDataPath("/h")
	got, ok := fs.files[path]
	if !ok {
		t.Fatalf("refresh did not write the Work store doc at %s", path)
	}
	if !bytes.Equal(got, workStoreDoc) {
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

	path := workStoreDataPath("/h")
	stale := []byte("# hand-edited stale copy\n\nconsult pop docs.\n")
	fs.files[path] = append([]byte{}, stale...)

	_, real := fakeFactories("/h", fs)
	if warnings := ensureForRevisionWith("rev-seed2", testConfigDeps(t), real); warnings != nil {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	if !bytes.Equal(fs.files[path], workStoreDoc) {
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

	path := workStoreDataPath("/h")
	fs.files[path] = append([]byte{}, workStoreDoc...)
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
		t.Errorf("refresh wrote work-store.md when bytes already matched, got %d writes", workStoreWrites)
	}
}
