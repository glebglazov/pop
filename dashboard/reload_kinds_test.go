package dashboard

import "testing"

// TestReload_RenewsKindAdapters pins ADR-0242 decision 4: the adapters a verb, a
// menu or an artifact listing resolves through are the ones the last applied
// reload built, so their per-load git memo is never older than the rows on
// screen. Renewal is checked by identity because that is the whole property —
// a kind carried over from open would answer about the repository as it was
// then, whatever it says now.
func TestReload_RenewsKindAdapters(t *testing.T) {
	d, cfg, _, _ := taskSetPaneFixture(t)
	m := openFromPane(t, d, cfg)

	before := m.kinds
	if len(before.byID) == 0 {
		t.Fatal("the fixture model holds no kinds to renew")
	}

	msg, ok := m.reload()().(dashboardRowsMsg)
	if !ok {
		t.Fatal("reload did not produce a rows message")
	}
	if len(msg.kinds) == 0 {
		t.Fatal("the reload carried no kind list: the model has nothing to renew from")
	}
	updated, _ := m.Update(msg)
	m = updated.(QueueDashboard)

	if len(m.kinds.byID) != len(before.byID) {
		t.Fatalf("kinds after reload = %d, want the same %d the page builds", len(m.kinds.byID), len(before.byID))
	}
	for _, k := range msg.kinds {
		if k == nil {
			continue
		}
		held, ok := m.kinds.byID[k.ID()]
		if !ok {
			t.Fatalf("kind %q the reload built is not the one the model holds", k.ID())
		}
		if held != k {
			t.Fatalf("kind %q is not the adapter the reload built", k.ID())
		}
		if held == before.byID[k.ID()] {
			t.Fatalf("kind %q is still the open-time adapter: its git memo outlives its load", k.ID())
		}
	}
}

// TestReload_StaleResultKeepsKinds closes the seam between the two halves of
// ADR-0242: a dropped result must leave the adapters alone as well as the rows,
// or the model would answer verbs through kinds older than the snapshot beside
// them.
func TestReload_StaleResultKeepsKinds(t *testing.T) {
	d, cfg, _, _ := taskSetPaneFixture(t)
	m := openFromPane(t, d, cfg)

	first, ok := m.reload()().(dashboardRowsMsg)
	if !ok {
		t.Fatal("reload did not produce a rows message")
	}
	second, ok := m.reload()().(dashboardRowsMsg)
	if !ok {
		t.Fatal("reload did not produce a rows message")
	}

	updated, _ := m.Update(second)
	m = updated.(QueueDashboard)
	updated, _ = m.Update(first)
	m = updated.(QueueDashboard)

	for _, k := range second.kinds {
		if k == nil {
			continue
		}
		if m.kinds.byID[k.ID()] != k {
			t.Fatalf("kind %q came from the stale reload, want the newest applied one", k.ID())
		}
	}
}
