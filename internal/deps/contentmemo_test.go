package deps

import "testing"

// TestContentMemoEvictsAtCapacity pins the bound that makes a process-lifetime
// memo safe to hold in a daemon: past capacity the least-recently-used entry
// leaves, so the map's size is the working set rather than every content key the
// process has ever seen.
func TestContentMemoEvictsAtCapacity(t *testing.T) {
	memo := NewContentMemo[int](2)

	memo.Put("a", 1)
	memo.Put("b", 2)
	memo.Put("c", 3)

	if memo.Len() != 2 {
		t.Fatalf("len = %d past capacity 2", memo.Len())
	}
	if _, ok := memo.Get("a"); ok {
		t.Fatalf("oldest entry survived eviction")
	}
	for _, key := range []string{"b", "c"} {
		if _, ok := memo.Get(key); !ok {
			t.Fatalf("recent entry %q was evicted", key)
		}
	}

	// A hit is a use: it moves the entry out of eviction's way, which is what
	// keeps a set that every poll walks resident while a one-off walk ages out.
	if _, ok := memo.Get("b"); !ok {
		t.Fatalf("b missing before recency check")
	}
	memo.Put("d", 4)
	if _, ok := memo.Get("b"); !ok {
		t.Fatalf("recently used entry was evicted instead of the idle one")
	}
	if _, ok := memo.Get("c"); ok {
		t.Fatalf("idle entry survived eviction")
	}
}

func TestContentMemoZeroCapacityRetainsNothing(t *testing.T) {
	memo := NewContentMemo[int](0)
	memo.Put("a", 1)
	if _, ok := memo.Get("a"); ok || memo.Len() != 0 {
		t.Fatalf("zero-capacity memo retained an entry (len %d)", memo.Len())
	}
}

func TestContentMemoResetDropsEntries(t *testing.T) {
	memo := NewContentMemo[int](4)
	memo.Put("a", 1)
	memo.Reset()
	if _, ok := memo.Get("a"); ok || memo.Len() != 0 {
		t.Fatalf("reset kept an entry (len %d)", memo.Len())
	}
	// Reset leaves the memo usable, not just empty.
	memo.Put("b", 2)
	if v, ok := memo.Get("b"); !ok || v != 2 {
		t.Fatalf("memo unusable after reset: %v %v", v, ok)
	}
}
