package fanout

import (
	"errors"
	"runtime"
	"sync"
	"testing"
)

// TestMapKeepsInputOrder pins the property every caller depends on: which
// goroutine finishes first must not be visible in the results, or a Work
// snapshot's row order would change from run to run.
func TestMapKeepsInputOrder(t *testing.T) {
	items := []int{0, 1, 2, 3, 4, 5, 6, 7}
	// The later an item is, the sooner it finishes: a fan-out that collected
	// results as they arrived would come back reversed.
	got, err := Map(items, func(i int) (int, error) {
		spin(len(items) - i)
		return i * 10, nil
	})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	for i, want := range []int{0, 10, 20, 30, 40, 50, 60, 70} {
		if got[i] != want {
			t.Fatalf("results = %v, want input order %v", got, []int{0, 10, 20, 30, 40, 50, 60, 70})
		}
	}
}

// TestMapBoundsConcurrencyByGOMAXPROCS pins the bound as the machine's, not the
// input's: ten thousand groups must not become ten thousand concurrent reads.
func TestMapBoundsConcurrencyByGOMAXPROCS(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)

	var (
		mu      sync.Mutex
		live    int
		highest int
	)
	items := make([]int, 64)
	if _, err := Map(items, func(int) (int, error) {
		mu.Lock()
		live++
		if live > highest {
			highest = live
		}
		mu.Unlock()
		spin(1)
		mu.Lock()
		live--
		mu.Unlock()
		return 0, nil
	}); err != nil {
		t.Fatalf("Map: %v", err)
	}
	if highest > 2 {
		t.Fatalf("peak concurrent calls = %d, want at most GOMAXPROCS (2)", highest)
	}
}

// TestMapReportsFirstErrorInInputOrder pins that a failure reads the same as it
// would from a serial loop: the first item that failed, whichever failed first.
func TestMapReportsFirstErrorInInputOrder(t *testing.T) {
	third := errors.New("third")
	sixth := errors.New("sixth")
	visited := make([]bool, 8)
	var mu sync.Mutex
	_, err := Map([]int{0, 1, 2, 3, 4, 5, 6, 7}, func(i int) (int, error) {
		mu.Lock()
		visited[i] = true
		mu.Unlock()
		switch i {
		case 3:
			return 0, third
		case 6:
			return 0, sixth
		}
		return i, nil
	})
	if !errors.Is(err, third) {
		t.Fatalf("err = %v, want the lowest-indexed failure %v", err, third)
	}
	for i, seen := range visited {
		if !seen {
			t.Fatalf("item %d was never visited: a fan-out visits every item before reporting", i)
		}
	}
}

// TestMapOnOneProcessorIsASerialLoop pins the single-processor path: same
// answers, and the calls happen one after another.
func TestMapOnOneProcessorIsASerialLoop(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)

	var order []int
	got, err := Map([]int{1, 2, 3}, func(i int) (int, error) {
		order = append(order, i)
		return i, nil
	})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("results = %v, want [1 2 3]", got)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("call order = %v, want serial [1 2 3]", order)
	}
}

func TestMapOverNothing(t *testing.T) {
	got, err := Map(nil, func(int) (int, error) { return 0, errors.New("never called") })
	if err != nil || len(got) != 0 {
		t.Fatalf("Map(nil) = %v, %v; want no results and no error", got, err)
	}
}

// spin burns a little time without sleeping, so the test's timing does not
// depend on the scheduler honouring a duration.
func spin(units int) {
	total := 0
	for i := 0; i < units*200000; i++ {
		total += i
	}
	_ = total
}
