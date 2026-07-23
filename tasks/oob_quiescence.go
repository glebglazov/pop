package tasks

import (
	"context"
	"errors"
	"time"

	"github.com/glebglazov/pop/store"
)

// mutateWithCheckoutQuiescence runs mutate through the store's atomic quiescence
// gate (ADR-0104): the mutation commits only when the runtime checkout carries no
// live drain and no live Checkout gate hold, with the check and the write in one
// transaction so a concurrent BeginDrain cannot interleave. Liveness uses the
// same PID+start-token standard as drains, so a dead-owner drain row or orphan
// gate hold does not block. A refusal is translated into a clear, occupant-naming
// error.
func mutateWithCheckoutQuiescence(s *store.Store, runtimePath string, mutate func(ctx context.Context, ex store.Execer) error) error {
	occ, err := s.MutateIfCheckoutQuiescent(runtimePath, mutate)
	if err != nil {
		if errors.Is(err, store.ErrCheckoutBusy) {
			return checkoutBusyErr(occ)
		}
		return exitErr(ExitOperational, "checkout quiescence gate: %v", err)
	}
	return nil
}

// checkoutBusyErr renders the occupant that refused an out-of-band mutation into
// a clear error: it names what holds the checkout and points at the way to
// proceed (wait for a live drain to park/finish, or resolve the human-wait gate).
func checkoutBusyErr(occ *store.CheckoutOccupant) error {
	if occ == nil {
		return exitErr(ExitOperational, "checkout is not quiescent; retry once it is idle")
	}
	switch occ.Kind {
	case store.OccupantGateHold:
		return exitErr(ExitOperational,
			"checkout is held at a verification gate by set %q (PID %d since %s): resolve it at the interactive gate before accepting or remediating out of band",
			occ.SetID, occ.PID, occ.Since.Format(time.RFC3339))
	case store.OccupantWaiter:
		turn := "queued behind another waiter under the recovery turn"
		if occ.NextInTurn {
			turn = "next under the recovery turn (resume imminent)"
		}
		return exitErr(ExitOperational,
			"set %q is quota-recovering on this checkout (PID %d since %s), %s: it will resume and re-verify, so accepting or remediating out of band now would be overwritten — retry after it resumes or deregisters",
			occ.SetID, occ.PID, occ.Since.Format(time.RFC3339), turn)
	default:
		return exitErr(ExitOperational,
			"a drain is running on this checkout for set %q (PID %d since %s): retry after it parks or finishes",
			occ.SetID, occ.PID, occ.Since.Format(time.RFC3339))
	}
}
