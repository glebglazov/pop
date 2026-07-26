package cmd

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkStoreDoc_EmbeddedContent asserts the embedded pop Work store doc
// carries the three per-operation sections and uses spec.md throughout — never
// the legacy prd.md filename (ADR-0136).
func TestWorkStoreDoc_EmbeddedContent(t *testing.T) {
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
}

// TestSeedWorkStoreDoc_CreatesWhenAbsent covers the create-if-absent semantics:
// an empty machine writes the embedded doc verbatim to the XDG config path.
func TestSeedWorkStoreDoc_CreatesWhenAbsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	fs := newFakeFS()
	d := fakeDeps("/h", fs, io.Discard)

	if err := seedWorkStoreDoc(d); err != nil {
		t.Fatalf("seedWorkStoreDoc: %v", err)
	}
	want := filepath.Join("/h", ".config", "pop", "work-store.md")
	got, ok := fs.files[want]
	if !ok {
		t.Fatalf("expected seeded doc at %s, files: %v", want, sortedKeys(fs.files))
	}
	if !bytes.Equal(got, workStoreDoc) {
		t.Error("seeded doc bytes differ from the embedded doc")
	}
}

// TestSeedWorkStoreDoc_NeverOverwritesEditedFile covers the never-overwrite
// semantics: a user-edited file is left byte-identical, even across repeated
// seed calls (the machine-global override survives every refresh).
func TestSeedWorkStoreDoc_NeverOverwritesEditedFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	fs := newFakeFS()
	path := filepath.Join("/h", ".config", "pop", "work-store.md")
	edited := []byte("# my machine-global override\n")
	fs.files[path] = append([]byte{}, edited...)

	d := fakeDeps("/h", fs, io.Discard)
	for i := 0; i < 3; i++ {
		if err := seedWorkStoreDoc(d); err != nil {
			t.Fatalf("seedWorkStoreDoc (pass %d): %v", i, err)
		}
	}
	if !bytes.Equal(fs.files[path], edited) {
		t.Errorf("edited doc was not left byte-identical: %q", fs.files[path])
	}
}

// TestSeedWorkStoreDoc_RespectsXDGConfigHome resolves the doc under
// $XDG_CONFIG_HOME/pop, not the home fallback, when the env var is set.
func TestSeedWorkStoreDoc_RespectsXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	fs := newFakeFS()
	d := fakeDeps("/h", fs, io.Discard)

	if err := seedWorkStoreDoc(d); err != nil {
		t.Fatalf("seedWorkStoreDoc: %v", err)
	}
	want := filepath.Join("/cfg", "pop", "work-store.md")
	if _, ok := fs.files[want]; !ok {
		t.Fatalf("expected seeded doc at %s, files: %v", want, sortedKeys(fs.files))
	}
	if _, ok := fs.files[filepath.Join("/h", ".config", "pop", "work-store.md")]; ok {
		t.Error("doc must not be seeded under the home fallback when XDG_CONFIG_HOME is set")
	}
}

// TestRefresh_SeedsWorkStoreDocOnceAcrossAgents proves the seed rides Integration
// refresh, is agent-agnostic, and is written once regardless of how many agents
// are integrated.
func TestRefresh_SeedsWorkStoreDocOnceAcrossAgents(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	installViaFake(t, fs, "/h", "pi")

	dry, real := fakeFactories("/h", fs)
	if warnings := ensureIntegrationsForRevisionWith("rev-seed1", dry, real); warnings != nil {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	path := filepath.Join("/h", ".config", "pop", "work-store.md")
	got, ok := fs.files[path]
	if !ok {
		t.Fatalf("refresh did not seed the Work store doc at %s", path)
	}
	if !bytes.Equal(got, workStoreDoc) {
		t.Error("seeded doc bytes differ from the embedded doc")
	}
}

// TestRefresh_LeavesEditedWorkStoreDocByteIdentical proves an edited doc survives
// a subsequent refresh unchanged — user edits are the machine-global override.
func TestRefresh_LeavesEditedWorkStoreDocByteIdentical(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")

	path := filepath.Join("/h", ".config", "pop", "work-store.md")
	edited := []byte("# hand-edited override\n\nconsult pop docs.\n")
	fs.files[path] = append([]byte{}, edited...)

	dry, real := fakeFactories("/h", fs)
	if warnings := ensureIntegrationsForRevisionWith("rev-seed2", dry, real); warnings != nil {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	if !bytes.Equal(fs.files[path], edited) {
		t.Errorf("refresh overwrote an edited Work store doc: %q", fs.files[path])
	}
}
