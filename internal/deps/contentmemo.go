package deps

import (
	"container/list"
	"sync"
)

// ContentMemo memoizes the answer to a pure function of files under a key that
// spells out the content that function read. It is the counterpart to MemoGit
// for the other half of a Work read's cost: where a git fact is a question about
// a moment and so may only be cached for one load, a derivation whose inputs are
// entirely named by its key cannot go stale — a changed file changes the key and
// misses. That is what lets this memo outlive a load and serve every later poll.
//
// Outliving a load is exactly why it must be bounded: the daemon holds one for
// its whole life, and an unbounded map keyed on content would grow with every
// edit a human makes over a day. Entries are evicted least-recently-used at
// capacity, so the memo's size is the working set of whatever it caches rather
// than the history of it.
//
// It is safe for concurrent use. A caller storing a mutable value must store its
// own copy and copy again on serve — the memo hands the same value to every
// hit and never copies for you.
type ContentMemo[V any] struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*list.Element
	// order holds *memoRecord[V] most-recently-used first, so eviction is the
	// back of the list.
	order *list.List
}

type memoRecord[V any] struct {
	key   string
	value V
}

// NewContentMemo returns a memo holding at most capacity entries. A capacity of
// zero or less retains nothing: every Get misses, which is how a caller turns
// memoization off without a second code path.
func NewContentMemo[V any](capacity int) *ContentMemo[V] {
	return &ContentMemo[V]{
		capacity: capacity,
		entries:  map[string]*list.Element{},
		order:    list.New(),
	}
}

// Capacity is the entry bound this memo evicts at.
func (c *ContentMemo[V]) Capacity() int { return c.capacity }

// Len is how many entries the memo currently holds, never above Capacity.
func (c *ContentMemo[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *ContentMemo[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*memoRecord[V]).value, true
}

func (c *ContentMemo[V]) Put(key string, value V) {
	if c.capacity <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		el.Value.(*memoRecord[V]).value = value
		c.order.MoveToFront(el)
		return
	}
	c.entries[key] = c.order.PushFront(&memoRecord[V]{key: key, value: value})
	for len(c.entries) > c.capacity {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*memoRecord[V]).key)
	}
}

// Reset drops every entry. Production has no reason to call it — a content key
// is never wrong — but a test that must observe a cold read needs one.
func (c *ContentMemo[V]) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]*list.Element{}
	c.order.Init()
}
