package tasks

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

const spliceTestThreadID = "01a0106f-3385-7371-9186-ce7ee43972b6"

// codexHomeDeps builds Deps over the real filesystem with CODEX_HOME routed to
// a fake codex home, so the whole join runs against a directory the test owns.
func codexHomeDeps(codexHome string) *Deps {
	real := deps.NewRealFileSystem()
	return &Deps{
		Git: deps.NewRealGit(),
		FS: &deps.MockFileSystem{
			GetenvFunc: func(key string) string {
				if key == "CODEX_HOME" {
					return codexHome
				}
				return ""
			},
			GetwdFunc:        real.Getwd,
			UserHomeDirFunc:  func() (string, error) { return filepath.Join(codexHome, "unused-home"), nil },
			StatFunc:         real.Stat,
			ReadDirFunc:      real.ReadDir,
			ReadFileFunc:     real.ReadFile,
			WriteFileFunc:    real.WriteFile,
			MkdirAllFunc:     real.MkdirAll,
			RenameFunc:       real.Rename,
			RemoveAllFunc:    real.RemoveAll,
			DirFSFunc:        real.DirFS,
			EvalSymlinksFunc: real.EvalSymlinks,
		},
	}
}

// writeFakeRollout lays down one date-sharded rollout file named after the
// thread id, exactly as codex names its own.
func writeFakeRollout(t *testing.T, codexHome, threadID, body string, perm os.FileMode) string {
	t.Helper()
	dir := filepath.Join(codexHome, "sessions", "2026", "08", "17")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-08-17T17-55-18-"+threadID+".jsonl")
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
	return path
}

// codexRunRecorder records a minimal codex exec stream: the thread.started that
// carries the join key, one item, and the whole-run turn.completed rollup.
func codexRunRecorder(t *testing.T, start time.Time) *streamRecorder {
	t.Helper()
	rec := newStreamRecorder(io.Discard, fakeClock(start, time.Second))
	lines := `{"type":"thread.started","thread_id":"` + spliceTestThreadID + `"}` + "\n" +
		`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":10}}` + "\n"
	if _, err := rec.Write([]byte(lines)); err != nil {
		t.Fatal(err)
	}
	rec.finish()
	return rec
}

const fakeRolloutBody = `{"timestamp":"2026-08-17T17:55:19.000Z","type":"session_meta","payload":{"session_id":"` + spliceTestThreadID + `"}}
{"timestamp":"2026-08-17T17:55:20.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":17806,"total_tokens":18011},"last_token_usage":{"input_tokens":17806,"cached_input_tokens":11008,"output_tokens":205,"total_tokens":18011},"model_context_window":258400}}}
{"timestamp":"2026-08-17T17:55:25.000Z","type":"event_msg","payload":{"type":"agent_message","message":"hello"}}
{"timestamp":"2026-08-17T17:55:30.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":36856,"total_tokens":37393},"last_token_usage":{"input_tokens":19050,"cached_input_tokens":17152,"output_tokens":332,"total_tokens":19382},"model_context_window":258400}}}
`

// persistCodexRun persists one codex run through the real chokepoint and
// returns its stored events.
func persistCodexRun(t *testing.T, d *Deps, start time.Time, agent string) []map[string]any {
	t.Helper()
	taskSetDir := t.TempDir()
	rec := codexRunRecorder(t, start)
	_, eventsPath, err := writeCapturedRun(d, taskSetDir, "implement", "demo", "01-a", "01-a.md", rec, agent, agent+" exec", "", 1, streamOutcomeCompleted, "", 0, "", "")
	if err != nil {
		t.Fatalf("persist %s run: %v", agent, err)
	}
	return readCapturedRunEvents(t, eventsPath)
}

func tokenCountEvents(t *testing.T, events []map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, ev := range events {
		raw, _ := ev["raw"].(string)
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		if payload["type"] == "token_count" {
			out = append(out, payload)
		}
	}
	return out
}

func TestPersistedCodexRunCarriesRolloutTokenCounts(t *testing.T) {
	codexHome := t.TempDir()
	writeFakeRollout(t, codexHome, spliceTestThreadID, fakeRolloutBody, 0o644)
	start := time.Date(2026, 8, 17, 17, 55, 18, 0, time.UTC)

	events := persistCodexRun(t, codexHomeDeps(codexHome), start, "codex")

	if len(events) != 5 {
		t.Fatalf("stored %d events, want 3 own + 2 spliced: %+v", len(events), events)
	}
	spliced := tokenCountEvents(t, events)
	if len(spliced) != 2 {
		t.Fatalf("spliced %d token_count events, want 2", len(spliced))
	}
	// The rollout's own payload shape survives the splice: a later reader sees
	// codex's vocabulary, not a pop invention.
	info, ok := spliced[1]["info"].(map[string]any)
	if !ok {
		t.Fatalf("spliced event lost its info block: %+v", spliced[1])
	}
	last, ok := info["last_token_usage"].(map[string]any)
	if !ok || last["input_tokens"] != float64(19050) {
		t.Fatalf("last_token_usage = %+v", info["last_token_usage"])
	}
	if info["model_context_window"] != float64(258400) {
		t.Fatalf("model_context_window = %v", info["model_context_window"])
	}
	// Only token_count crosses; the rollout's agent_message stays behind.
	for _, ev := range events {
		if raw, _ := ev["raw"].(string); raw == `{"type":"agent_message","message":"hello"}` {
			t.Fatalf("non-token_count rollout event was spliced: %+v", events)
		}
	}
	// Order relative to the run's own events is deterministic: thread.started
	// (t+0s) first, the rollout's calls at t+2s and t+12s after the run's own
	// events at t+1s and t+2s.
	firstRaw, _ := events[0]["raw"].(string)
	lastRaw, _ := events[len(events)-1]["raw"].(string)
	if firstRaw != `{"type":"thread.started","thread_id":"`+spliceTestThreadID+`"}` {
		t.Fatalf("first stored event = %s", firstRaw)
	}
	var lastPayload map[string]any
	if err := json.Unmarshal([]byte(lastRaw), &lastPayload); err != nil || lastPayload["type"] != "token_count" {
		t.Fatalf("last stored event = %s", lastRaw)
	}
}

func TestPersistedCodexRunWithoutRolloutStoresUnspliced(t *testing.T) {
	start := time.Date(2026, 8, 17, 17, 55, 18, 0, time.UTC)

	// No sessions directory at all.
	bare := persistCodexRun(t, codexHomeDeps(t.TempDir()), start, "codex")
	if len(bare) != 3 || len(tokenCountEvents(t, bare)) != 0 {
		t.Fatalf("run with no sessions directory stored %d events: %+v", len(bare), bare)
	}

	// A sessions directory holding some other thread's rollout.
	otherHome := t.TempDir()
	writeFakeRollout(t, otherHome, "0199ffff-0000-7000-8000-000000000000", fakeRolloutBody, 0o644)
	mismatched := persistCodexRun(t, codexHomeDeps(otherHome), start, "codex")
	if len(mismatched) != 3 || len(tokenCountEvents(t, mismatched)) != 0 {
		t.Fatalf("run with a mismatched rollout stored %d events: %+v", len(mismatched), mismatched)
	}
}

func TestPersistedCodexRunWithUnreadableRolloutStoresUnspliced(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	codexHome := t.TempDir()
	writeFakeRollout(t, codexHome, spliceTestThreadID, fakeRolloutBody, 0o000)
	start := time.Date(2026, 8, 17, 17, 55, 18, 0, time.UTC)

	events := persistCodexRun(t, codexHomeDeps(codexHome), start, "codex")

	if len(events) != 3 || len(tokenCountEvents(t, events)) != 0 {
		t.Fatalf("run with an unreadable rollout stored %d events: %+v", len(events), events)
	}
}

func TestNonCodexRunNeverJoinsTheRollout(t *testing.T) {
	codexHome := t.TempDir()
	writeFakeRollout(t, codexHome, spliceTestThreadID, fakeRolloutBody, 0o644)
	start := time.Date(2026, 8, 17, 17, 55, 18, 0, time.UTC)

	d := codexHomeDeps(codexHome)
	reads := 0
	inner := d.FS.(*deps.MockFileSystem)
	underlying := inner.ReadDirFunc
	inner.ReadDirFunc = func(path string) ([]os.DirEntry, error) {
		if strings.HasPrefix(path, filepath.Join(codexHome, "sessions")) {
			reads++
		}
		return underlying(path)
	}

	events := persistCodexRun(t, d, start, "claude")

	if len(events) != 3 || len(tokenCountEvents(t, events)) != 0 {
		t.Fatalf("claude run stored %d events: %+v", len(events), events)
	}
	if reads != 0 {
		t.Fatalf("claude run walked codex's sessions directory %d times", reads)
	}
}
