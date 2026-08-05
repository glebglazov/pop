// Package fanout runs one read over many independent inputs at once, bounded by
// the machine rather than by how many inputs there are.
//
// It exists for the Work read path (ADR-0189), where a cold first paint used to
// cost the sum of its repository groups' loads. Every load here must be a pure
// read of independent state: the caller decides what is safe to run at once, and
// the bound only decides how much of it runs at the same moment.
package fanout

import (
	"runtime"
	"sync"
)

// Map applies fn to every item at once, bounded at GOMAXPROCS concurrent calls,
// and returns the results in input order together with the first error in input
// order — the error a serial loop over the same items would have returned.
//
// Unlike a serial loop it does not stop at the first failure: every item is
// visited, and only then is the lowest-indexed error returned. That is what
// makes the fan-out unobservable to a caller reading pure state, and it is the
// reason fn must be one — a fn with side effects would perform them for items a
// serial loop would never have reached.
//
// fn runs on many goroutines, so whatever it closes over must be safe for
// concurrent use. Each call writes only its own slot in the result slice, so the
// results themselves need no synchronisation.
func Map[T, R any](items []T, fn func(T) (R, error)) ([]R, error) {
	results := make([]R, len(items))
	errs := make([]error, len(items))

	workers := runtime.GOMAXPROCS(0)
	if workers > len(items) {
		workers = len(items)
	}
	if workers <= 1 {
		// One worker is a serial loop, and written as one: a machine pinned to a
		// single processor should not pay for goroutines and a channel to arrive at
		// the same order of calls.
		for i, item := range items {
			results[i], errs[i] = fn(item)
		}
		return results, firstError(errs)
	}

	indexes := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indexes {
				results[i], errs[i] = fn(items[i])
			}
		}()
	}
	for i := range items {
		indexes <- i
	}
	close(indexes)
	wg.Wait()

	return results, firstError(errs)
}

// firstError returns the lowest-indexed error, so which failure a caller sees
// depends on its input order and not on which goroutine finished first.
func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
