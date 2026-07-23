package tasks

import "github.com/glebglazov/pop/store"

// ReadCheckoutClaim derives the live Checkout claim on runtimePath, or nil when
// nothing live claims it (ADR-0135): a running Drain, a live Recovery waiter, or
// a claim-bearing Failed-gate hold. It is the tasks-layer wrapper over the
// store's read-time claim derivation, used by queue dispatch to defer a Ready
// set whose bound checkout another set already claims. A missing store or read
// error yields no claim so dispatch degrades to spawnable (the transactional
// BeginDrain chokepoint still refuses a genuine double-spawn) rather than
// blocking on a transient store problem.
func ReadCheckoutClaim(d *Deps, runtimePath string) (*store.CheckoutClaim, error) {
	if d == nil || runtimePath == "" {
		return nil, nil
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	return s.ReadCheckoutClaim(runtimePath)
}
