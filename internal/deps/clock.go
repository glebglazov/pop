package deps

import (
	"math/rand"
	"time"
)

// Clock is the current-instant seam. Derivations stay pure by taking `now` as an
// argument; this seam is for the one caller above them that has to produce it, so
// a test fixes a weekday instead of skipping cases two days a week.
type Clock interface {
	// Now returns the current instant in its own location, the way time.Now does:
	// a derivation that asks "what weekday is it" wants the human's date, not UTC's.
	Now() time.Time
}

// RealClock reads the wall clock.
type RealClock struct{}

func NewRealClock() RealClock { return RealClock{} }

func (RealClock) Now() time.Time { return time.Now() }

// FixedClock is a test double whose Now never moves.
type FixedClock struct {
	Instant time.Time
}

func (c FixedClock) Now() time.Time { return c.Instant }

// Rand is the randomness seam, shaped so *math/rand.Rand satisfies it: a test
// passes rand.New(rand.NewSource(seed)) and asserts an exact result rather than
// tolerating a range.
type Rand interface {
	// Int63n returns a uniform value in [0, n). It panics for n <= 0.
	Int63n(n int64) int64
}

// NewRealRand returns a Rand seeded from the wall clock.
func NewRealRand() Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}
