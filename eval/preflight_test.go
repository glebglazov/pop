package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneAtCommitSetsHarnessIdentityAndDisablesSigning(t *testing.T) {
	source := t.TempDir()
	runGit(t, source, "init", "-q")
	runGit(t, source, "config", "user.name", "Source Author")
	runGit(t, source, "config", "user.email", "source@example.test")
	writeFile(t, filepath.Join(source, "file.txt"), "parent\n")
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "-qm", "parent")
	parent := runGit(t, source, "rev-parse", "HEAD")

	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".gitconfig"), "[commit]\n\tgpgsign = true\n")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clone := filepath.Join(t.TempDir(), "clone")
	if err := cloneAtCommit(source, parent, clone); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"user.name": "Pop Eval Harness", "user.email": "eval@pop.invalid", "commit.gpgsign": "false",
	} {
		if got := runGit(t, clone, "config", "--local", "--get", key); got != want {
			t.Fatalf("local %s = %q; want %q", key, got, want)
		}
	}
	writeFile(t, filepath.Join(clone, "file.txt"), "changed\n")
	runGit(t, clone, "add", ".")
	runGit(t, clone, "commit", "-qm", "Trial implementation")
	if got := runGit(t, clone, "show", "-s", "--format=%an <%ae> %G?", "HEAD"); got != "Pop Eval Harness <eval@pop.invalid> N" {
		t.Fatalf("commit identity and signature = %q", got)
	}
}

func TestMatrixPreflightStopsBeforeTrialCreation(t *testing.T) {
	root := t.TempDir()
	cases, arms := filepath.Join(root, "cases"), filepath.Join(root, "arms")
	caseDir := filepath.Join(cases, "example")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(caseManifest{Name: "example", RepositoryURL: filepath.Join(root, "missing-repository"), ParentCommit: "abc", ReferenceRange: "abc..def"})
	writeFile(t, filepath.Join(caseDir, caseManifestName), string(manifest))
	writeFile(t, filepath.Join(caseDir, acceptanceName), "Status: approved\n\n1. Work is complete.\n")
	if err := os.MkdirAll(arms, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(arms, "missing.toml"), "kind = 'bare'\nagent = 'claude'\nmodel = 'test'\n")
	t.Setenv("PATH", t.TempDir())
	work, results := filepath.Join(root, "work"), filepath.Join(root, "results")
	err := runTrialCommandWithProgress([]string{"--case", "example", "--arm", "missing", "--cases", cases, "--arms", arms, "--work", work, "--results", results}, evalProgress{})
	if err == nil || !strings.Contains(err.Error(), `Arm "missing"`) || !strings.Contains(err.Error(), `binary "claude"`) {
		t.Fatalf("preflight error = %v", err)
	}
	for _, path := range []string{work, results} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("preflight created %s", path)
		}
	}
}

func TestMatrixPreflightChecksPopAndWritableRoots(t *testing.T) {
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "claude"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	arms := map[string]armFile{"pop-arm": {Kind: "pop", Agent: "claude", Model: "test"}}
	err := preflightMatrix([]string{"pop-arm"}, arms, "missing-pop", filepath.Join(t.TempDir(), "work"), filepath.Join(t.TempDir(), "results"))
	if err == nil || !strings.Contains(err.Error(), `Pop binary "missing-pop"`) {
		t.Fatalf("Pop preflight error = %v", err)
	}

	for _, failing := range []string{"work", "results"} {
		t.Run(failing, func(t *testing.T) {
			root := t.TempDir()
			work, results := filepath.Join(root, "work"), filepath.Join(root, "results")
			blocked := work
			if failing == "results" {
				blocked = results
			}
			writeFile(t, blocked, "not a directory")
			err := preflightMatrix([]string{"bare"}, map[string]armFile{"bare": {Kind: "bare", Agent: "claude", Model: "test"}}, "pop", work, results)
			if err == nil || !strings.Contains(err.Error(), "preflight "+failing+" root") || !strings.Contains(err.Error(), "not writable") {
				t.Fatalf("%s preflight error = %v", failing, err)
			}
			if _, statErr := os.Stat(filepath.Join(results, "example", "bare", "01", "trial.json")); statErr == nil {
				t.Fatal("preflight created a Trial record")
			}
		})
	}
}
