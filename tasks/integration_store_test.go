package tasks

import (
	"testing"
	"time"
)

// TestRecordIntegrationEvent verifies the durable integration event is appended
// and read back newest-first, carrying base_ref and branch_sha provenance.
func TestRecordIntegrationEvent(t *testing.T) {
	d := bindingsStoreDeps(t)
	key := "repo-abc\x00set-1"

	first := IntegrationEvent{
		ScopedKey:    key,
		SetID:        "set-1",
		Project:      "proj",
		IntegratedAt: time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
		BaseRef:      "aaaa",
		BranchSHA:    "bbbb",
	}
	if err := RecordIntegrationEvent(d, first); err != nil {
		t.Fatalf("record first: %v", err)
	}
	second := IntegrationEvent{
		ScopedKey:    key,
		SetID:        "set-1",
		Project:      "proj",
		IntegratedAt: time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
		BaseRef:      "cccc",
		BranchSHA:    "dddd",
	}
	if err := RecordIntegrationEvent(d, second); err != nil {
		t.Fatalf("record second: %v", err)
	}

	events, err := IntegrationEventsForSet(d, "set-1")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2", events)
	}
	// Newest first.
	if events[0].BaseRef != "cccc" || events[0].BranchSHA != "dddd" {
		t.Fatalf("latest event = %+v, want base cccc branch dddd", events[0])
	}
	if events[1].BaseRef != "aaaa" {
		t.Fatalf("oldest event = %+v, want base aaaa", events[1])
	}

	all, err := AllIntegrationEvents(d)
	if err != nil || len(all) != 2 {
		t.Fatalf("all events = %+v err = %v, want 2", all, err)
	}

	none, err := IntegrationEventsForSet(d, "missing")
	if err != nil || len(none) != 0 {
		t.Fatalf("events for missing set = %+v err = %v, want empty", none, err)
	}
}

// TestIntegrationEventsNoStore confirms a pure reader with no store materialises
// nothing and returns no events.
func TestIntegrationEventsNoStore(t *testing.T) {
	d := bindingsStoreDeps(t)
	events, err := IntegrationEventsForSet(d, "set-1")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want empty", events)
	}
}
