package tasks

import (
	"fmt"
	"time"

	"github.com/glebglazov/pop/work/ref"
)

// A Task set's Mute at the tasks layer (ADR-0200). Unlike archive and priority
// these do not go through the global state lock: a mute lives on the cross-kind
// registry row, which UpdateGlobalStateWith never rewrites, so there is nothing
// for a concurrent registration reconcile to lose. They write the store
// directly, the way the Checkout-claim read does.

// MuteTaskSet records a human's "not now" on one registered set until the given
// instant, secret marking the random default window. Muting also clears the
// set's Auto-drain consent, and that is the whole of mute's reach into
// supervision: the daemon admits only Ready && AutoDrain sets, so nothing in the
// supervision path has to learn what a mute is (ADR-0200 decision 2).
//
// The clear is a write made once, not a gate. Unmuting will not give the bit
// back, and the human may turn Auto-drain on again while the set is still muted.
func MuteTaskSet(d *Deps, defPath, taskSetID string, until time.Time, secret bool) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	if err := s.MuteWorkContainer(taskSetRef(taskSetID), until, secret); err != nil {
		return err
	}
	if defPath == "" {
		return fmt.Errorf("tasks: %s carries no definition path to clear auto-drain against", taskSetID)
	}
	_, err = SetTaskSetAutoDrain(d, defPath, taskSetID, false)
	return err
}

// UnmuteTaskSet clears one registered set's mute. It restores nothing: the
// Auto-drain bit muting destroyed stays destroyed, and is not even reported,
// because standing consent to act is an instruction only the human gives.
func UnmuteTaskSet(d *Deps, taskSetID string) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	return s.UnmuteWorkContainer(taskSetRef(taskSetID))
}

func taskSetRef(taskSetID string) ref.WorkRef {
	return ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: taskSetID}
}
