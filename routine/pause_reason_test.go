package routine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readManifest(t *testing.T, d *Deps, id string) Manifest {
	t.Helper()
	data, err := d.FS.ReadFile(filepath.Join(routineDir(d, id), manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAddWritesCreatedPauseReason(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "daily", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if got := readManifest(t, d, "daily").PauseReason; got != PauseReasonCreated {
		t.Fatalf("PauseReason = %q, want %q", got, PauseReasonCreated)
	}
}

func TestPauseWritesManualResumeClears(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "daily", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeWith(d, "daily"); err != nil {
		t.Fatal(err)
	}
	if got := readManifest(t, d, "daily").PauseReason; got != "" {
		t.Fatalf("after resume PauseReason = %q, want empty", got)
	}
	if _, err := PauseWith(d, "daily"); err != nil {
		t.Fatal(err)
	}
	if got := readManifest(t, d, "daily").PauseReason; got != PauseReasonManual {
		t.Fatalf("after pause PauseReason = %q, want %q", got, PauseReasonManual)
	}
}

func TestFireFailurePausesWithFailureReason(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 2)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "fail-me", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// Resume first so the pause we observe is the failure trigger, not the
	// created-paused state.
	if _, err := ResumeWith(d, "fail-me"); err != nil {
		t.Fatal(err)
	}

	if _, err := FireWith(d, "fail-me"); err == nil {
		t.Fatal("expected fire failure")
	}

	m := readManifest(t, d, "fail-me")
	if !m.Paused {
		t.Fatal("failed run should pause the routine")
	}
	if m.PauseReason != PauseReasonFailure {
		t.Fatalf("PauseReason = %q, want %q", m.PauseReason, PauseReasonFailure)
	}
}

func TestFireFailureOverwritesEarlierPauseReason(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 2)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "already", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// Created paused (reason=created); a failed manual fire overwrites the
	// reason with the latest cause.
	if _, err := FireWith(d, "already"); err == nil {
		t.Fatal("expected fire failure")
	}
	if got := readManifest(t, d, "already").PauseReason; got != PauseReasonFailure {
		t.Fatalf("PauseReason = %q, want %q (overwrite)", got, PauseReasonFailure)
	}
}

func TestFireSuccessDoesNotPause(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeClaude(t, root, 0)
	d := fireDeps(t, dataHome)

	if _, err := AddWith(d, "ok", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeWith(d, "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := FireWith(d, "ok"); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, d, "ok")
	if m.Paused {
		t.Fatal("successful run should not pause the routine")
	}
	if m.PauseReason != "" {
		t.Fatalf("PauseReason = %q, want empty", m.PauseReason)
	}
}

func TestPausedStatusLabel(t *testing.T) {
	cases := []struct {
		reason PauseReason
		want   string
	}{
		{PauseReasonCreated, "paused"},
		{PauseReasonManual, "paused"},
		{"", "paused"},
		{PauseReasonFailure, "paused (failed)"},
		{PauseReasonChanged, "paused (changed)"},
	}
	for _, c := range cases {
		if got := pausedStatusLabel(c.reason); got != c.want {
			t.Fatalf("pausedStatusLabel(%q) = %q, want %q", c.reason, got, c.want)
		}
	}
}

func TestDashboardIdleStatusRendersReason(t *testing.T) {
	cases := []struct {
		m    Manifest
		want string
	}{
		{Manifest{Paused: false}, "idle"},
		{Manifest{Paused: true, PauseReason: PauseReasonManual}, "paused"},
		{Manifest{Paused: true, PauseReason: PauseReasonFailure}, "paused (failed)"},
		{Manifest{Paused: true, PauseReason: PauseReasonChanged}, "paused (changed)"},
		// Legacy manifest without the field reads plain paused.
		{Manifest{Paused: true}, "paused"},
	}
	for _, c := range cases {
		if got := dashboardIdleStatus(c.m, ""); got != c.want {
			t.Fatalf("dashboardIdleStatus(%+v) = %q, want %q", c.m, got, c.want)
		}
	}
}

func TestRefineMenuHeaderShowsPauseReason(t *testing.T) {
	var out bytes.Buffer
	r := &Routine{
		ID:       "daily",
		Manifest: Manifest{Paused: true, PauseReason: PauseReasonFailure, Schedule: "every 6h"},
	}
	renderRefineMenu(&out, "daily", r, "no runs yet")
	if !strings.Contains(out.String(), "paused (failed)") {
		t.Fatalf("refine header missing pause reason:\n%s", out.String())
	}
}

func TestLegacyManifestLoadsAsPlainPaused(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "legacy", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// Rewrite the manifest without the pause_reason field to mimic a routine
	// created before ADR-0128.
	path := filepath.Join(routineDir(d, "legacy"), manifestFileName)
	legacy := `{"bound_directory":"` + canonical(t, home) + `","schedule":"every 6h","paused":true,"created_at":"2026-07-18T12:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := loadManifest(d, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Manifest.Paused {
		t.Fatal("legacy routine should load paused")
	}
	if r.Manifest.PauseReason != "" {
		t.Fatalf("PauseReason = %q, want empty", r.Manifest.PauseReason)
	}
	if got := dashboardIdleStatus(r.Manifest, ""); got != "paused" {
		t.Fatalf("legacy status = %q, want plain paused", got)
	}
}
