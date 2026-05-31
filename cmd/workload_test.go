package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/workload"
)

func TestWorkloadStatusExitSuccessWithMalformedRows(t *testing.T) {
	root := t.TempDir()
	prdPath := filepath.Join(root, "thoughts/prds/bad.md")
	if err := os.MkdirAll(filepath.Dir(prdPath), 0o755); err != nil {
		t.Fatal(err)
	}
	issueDir := filepath.Join(root, "thoughts/issues/bad")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# Bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	d := workload.DefaultDeps()
	var buf bytes.Buffer
	if err := runWorkloadStatusWith(d, &buf); err != nil {
		t.Fatalf("status should succeed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}

func TestWorkloadStatusUnreadableDiscoveryFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	prdDir := filepath.Join(root, "thoughts/prds")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(prdDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(prdDir, 0o755) })

	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	err := runWorkloadStatusWith(workload.DefaultDeps(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected setup failure")
	}
}

func TestWorkloadSetPriorityRefreshesTable(t *testing.T) {
	root := t.TempDir()
	workloadProject = ""
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	prdPath := filepath.Join(root, "thoughts/prds/feature.md")
	if err := os.MkdirAll(filepath.Dir(prdPath), 0o755); err != nil {
		t.Fatal(err)
	}
	issueDir := filepath.Join(root, "thoughts/issues/feature")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"issues":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# Feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	t.Setenv("XDG_DATA_HOME", root)
	if _, err := workload.RefreshWith(workload.DefaultDeps(), root, workload.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runWorkloadSetPriorityWith(workload.DefaultDeps(), &buf, "feature", "7"); err != nil {
		t.Fatalf("set-priority failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Updated priority for feature: 0 -> 7") {
		t.Fatalf("missing change report:\n%s", out)
	}
	if !strings.Contains(out, "7 AUTO") {
		t.Fatalf("missing refreshed table with AUTO:\n%s", out)
	}
}

func TestWorkloadStatusUsesDefinitionOverride(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "planning")
	prdPath := filepath.Join(defDir, "thoughts/prds/a.md")
	if err := os.MkdirAll(filepath.Dir(prdPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workloadProject = ""
	workloadPath = root
	workloadDefPath = defDir
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	t.Setenv("XDG_DATA_HOME", root)
	var buf bytes.Buffer
	if err := runWorkloadStatusWith(workload.DefaultDeps(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "a") {
		t.Fatalf("expected PRD in output:\n%s", buf.String())
	}
}

func TestWorkloadResolveByProjectName(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "svc")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkloadThoughts(t, projectDir, "svc")

	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("projects = [{ path = \""+projectDir+"\" }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origLoad := workloadConfigLoad
	workloadConfigLoad = func(path string) (*config.Config, error) {
		return config.Load(cfgPath)
	}
	t.Cleanup(func() { workloadConfigLoad = origLoad })

	workloadProject = "svc"
	workloadPath = ""
	workloadDefPath = ""
	t.Cleanup(func() {
		workloadProject = ""
		workloadPath = ""
		workloadDefPath = ""
	})

	t.Setenv("XDG_DATA_HOME", root)
	var buf bytes.Buffer
	if err := runWorkloadStatusWith(workload.DefaultDeps(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "svc") {
		t.Fatalf("expected PRD in output:\n%s", buf.String())
	}
}

func writeWorkloadThoughts(t *testing.T, dir, stem string) {
	t.Helper()
	prdPath := filepath.Join(dir, "thoughts/prds", stem+".md")
	if err := os.MkdirAll(filepath.Dir(prdPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# "+stem+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
