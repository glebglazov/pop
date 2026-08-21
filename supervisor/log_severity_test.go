package supervisor

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// TestBurnedRetryCapIsSevereWhateverTheDrainDid is the incident this ranking
// exists for: an agent spent every try it had on one task and a healthy agent
// picked the work up afterwards, so the drain finished cleanly and carries no
// ending at all. The attempts were still paid for, so the morning's listing has
// to show them — named by agent, task set and the work they were spent on
// (ADR-0231).
func TestBurnedRetryCapIsSevereWhateverTheDrainDid(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := initGitRepoWithBase(t)

	h, err := tasks.BeginDrain(td, repo, "rescued-set", nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	s, _, err := td.Store(true)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := s.RecordSpentRetryCap(store.SpentRetryCap{
		Repo:     repo,
		SetID:    "rescued-set",
		Phase:    "implement",
		TaskID:   "03-c",
		Preset:   "codex",
		Attempts: 3,
		Reason:   "API Error: Connection closed mid-response",
		SpentAt:  time.Now(),
	}); err != nil {
		t.Fatalf("RecordSpentRetryCap: %v", err)
	}
	// The rescue: the next agent finished the work, so the drain's own terminal
	// is the clean finish it really was and it records no ending.
	if err := h.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	events, err := BuildLog(td)
	if err != nil {
		t.Fatalf("BuildLog: %v", err)
	}
	var listing bytes.Buffer
	RenderSevereLog(&listing, events, 24*time.Hour, time.Now())
	got := listing.String()
	if want := "rescued-set retry_cap_spent agent=codex phase=implement task=03-c attempts=3"; !strings.Contains(got, want) {
		t.Fatalf("severe listing missing %q:\n%s", want, got)
	}
	if lines := strings.Count(strings.TrimSpace(got), "\n") + 1; lines != 1 {
		t.Fatalf("severe listing has %d lines, want just the burn:\n%s", lines, got)
	}
	// The drain beside it is routine: the process finished, and saying otherwise
	// would make the healthy half of the same night compete for attention.
	var journal bytes.Buffer
	RenderLog(&journal, events, 50)
	if strings.Contains(journal.String(), "SEVERE rescued-set finished") {
		t.Fatalf("journal marks the clean finish severe:\n%s", journal.String())
	}
}

// TestSevereJournalListingAnswersWhatWentWrong is the one question a human asks
// on coming back to the machine. The journal is otherwise flat, so the listing
// has to rank: a drain that spent its whole agent list and one that could not
// start an agent at all are what the human is looking for, and the healthy walk
// beside them — one agent stepping aside, the next finishing the work — must not
// compete with them for attention (ADR-0231).
func TestSevereJournalListingAnswersWhatWentWrong(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := initGitRepoWithBase(t)

	finish := func(setID string, ending store.DrainEnding) {
		t.Helper()
		h, err := tasks.BeginDrain(td, repo, setID, nil)
		if err != nil {
			t.Fatalf("BeginDrain %s: %v", setID, err)
		}
		if err := h.Finish(ending); err != nil {
			t.Fatalf("Finish %s: %v", setID, err)
		}
	}
	// The healthy walk: an agent stepped aside and the next one finished the work,
	// which is pop working and costs nothing.
	finish("healthy-set", store.DrainEnding{State: store.StateFinished})
	// The event this whole work stream exists for.
	finish("spent-set", store.DrainEnding{
		State:           store.StateFinished,
		Ending:          store.EndingAgentsExhausted,
		ExhaustedPreset: "claude",
	})
	// The no-op: nothing was attempted, so nothing failed — but the machine sat
	// idle, so the human still has to be told.
	finish("nothing-started-set", store.DrainEnding{
		State:           store.StateFinished,
		Ending:          store.EndingNoAgentStarted,
		ExhaustedPreset: "codex",
	})

	events, err := BuildLog(td)
	if err != nil {
		t.Fatalf("BuildLog: %v", err)
	}

	var listing bytes.Buffer
	RenderSevereLog(&listing, events, 24*time.Hour, time.Now())
	got := listing.String()
	for _, want := range []string{
		// Each entry names the agent and the task set, so the next action needs
		// nothing else opened.
		"spent-set agents_exhausted agent=claude",
		"nothing-started-set no_agent_started agent=codex",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("severe listing missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "healthy-set") {
		t.Fatalf("severe listing carries the healthy fall-through:\n%s", got)
	}
	if lines := strings.Count(strings.TrimSpace(got), "\n") + 1; lines != 2 {
		t.Fatalf("severe listing has %d lines, want just the two severe events:\n%s", lines, got)
	}

	// The whole journal still carries every event, with the severe ones marked.
	var journal bytes.Buffer
	RenderLog(&journal, events, 50)
	for _, want := range []string{
		"healthy-set finished",
		"SEVERE spent-set agents_exhausted agent=claude",
		"SEVERE nothing-started-set no_agent_started agent=codex",
	} {
		if !strings.Contains(journal.String(), want) {
			t.Fatalf("journal missing %q:\n%s", want, journal.String())
		}
	}
	if strings.Contains(journal.String(), "SEVERE healthy-set") {
		t.Fatalf("journal marks the healthy walk severe:\n%s", journal.String())
	}

	// The listing is a window, not the whole history: read from a day later,
	// today's severe events have aged out and the answer says so plainly.
	var later bytes.Buffer
	RenderSevereLog(&later, events, 24*time.Hour, time.Now().Add(72*time.Hour))
	if !strings.Contains(later.String(), "No severe events in the last 24h") {
		t.Fatalf("out-of-window listing = %q, want it to report an empty window", later.String())
	}
}
