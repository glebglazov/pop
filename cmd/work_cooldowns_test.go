package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// cooldownTestDeps is a tasks.Deps over an empty per-test store, plus the store
// itself for seeding rows.
func cooldownTestDeps(t *testing.T) (*tasks.Deps, *store.Store) {
	t.Helper()
	d := &tasks.Deps{
		FS:       cmdTestFS(filepath.Join(t.TempDir(), "xdg"), ""),
		LookPath: func(file string) (string, error) { return "/mock/bin/" + file, nil },
	}
	s, _, err := d.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	return d, s
}

// TestWorkCooldownsNamesGuessAndStatement pins the surface ADR-0235 asked for: a
// cooldown pop guessed says so, and says which window class it was dated from,
// where a provider-stated one reads as a plain reset.
func TestWorkCooldownsNamesGuessAndStatement(t *testing.T) {
	d, s := cooldownTestDeps(t)
	now := time.Date(2026, 8, 24, 19, 58, 0, 0, time.UTC)

	if _, err := s.RecordAgentQuotaCooldown(store.AgentCooldown{
		Preset:         "claude",
		ExhaustedUntil: now.Add(5 * time.Hour),
		Class:          string(tasks.QuotaWindowFiveHour),
		NextProbeAt:    now.Add(10 * time.Minute),
	}, now); err != nil {
		t.Fatal(err)
	}
	stated := now.Add(90 * time.Minute)
	if _, err := s.RecordAgentQuotaCooldown(store.AgentCooldown{
		Preset:         "codex",
		ExhaustedUntil: stated,
		StatedUntil:    stated,
	}, now); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runWorkCooldownsWith(d, &buf, now); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"claude    guessed",
		"session limit backstop",
		now.Add(10 * time.Minute).Local().Format(time.RFC3339),
		"codex     stated",
		stated.Local().Format(time.RFC3339),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cooldown listing missing %q:\n%s", want, got)
		}
	}
	// A stated row carries no window and no probe: nothing about it was invented,
	// and nothing will be asked about it.
	statedLine := ""
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "codex") {
			statedLine = line
		}
	}
	if strings.Contains(statedLine, "backstop") || !strings.Contains(statedLine, "-") {
		t.Fatalf("stated row reads as a guess: %q", statedLine)
	}
}

// TestWorkCooldownsClearDropsOne pins the other half: the named preset's
// cooldown is gone from the listing afterwards, an unknown preset is reported
// rather than silently succeeding, and neither one touches the other preset.
func TestWorkCooldownsClearDropsOne(t *testing.T) {
	d, s := cooldownTestDeps(t)
	now := time.Date(2026, 8, 24, 19, 58, 0, 0, time.UTC)
	for _, preset := range []string{"claude", "codex"} {
		if _, err := s.RecordAgentQuotaCooldown(store.AgentCooldown{
			Preset:         preset,
			ExhaustedUntil: now.Add(5 * time.Hour),
			Class:          string(tasks.QuotaWindowFiveHour),
		}, now); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := runWorkCooldownsClearWith(d, &buf, "claude", now); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "cleared claude quota cooldown") {
		t.Fatalf("clear said %q", got)
	}

	buf.Reset()
	if err := runWorkCooldownsWith(d, &buf, now); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "claude") {
		t.Fatalf("cleared preset still listed:\n%s", got)
	}
	if !strings.Contains(got, "codex") {
		t.Fatalf("clearing one preset dropped another:\n%s", got)
	}

	buf.Reset()
	if err := runWorkCooldownsClearWith(d, &buf, "claude", now); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "claude has no quota cooldown") {
		t.Fatalf("clearing an uncooled preset said %q", got)
	}
}

// An empty store is the common case, and it answers in words rather than with a
// bare header.
func TestWorkCooldownsWithNoneInForce(t *testing.T) {
	d, _ := cooldownTestDeps(t)
	var buf bytes.Buffer
	if err := runWorkCooldownsWith(d, &buf, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "No agent quota cooldowns are in force.") {
		t.Fatalf("empty listing said %q", got)
	}
}
