package tasks

// Unfolded reports whether a Task set is unfolded: its work is finished but
// the checkout is still held — bound, and DONE or Awaiting-approval. It is
// exactly the foldable condition (FoldEligibleStatus plus bound), named so a
// read surface can show it (ADR-0197). Derived at read time from fields already
// loaded for the worktree column; never persisted.
func Unfolded(bound bool, status TaskSetStatus) bool {
	return bound && FoldEligibleStatus(status)
}

// UnfoldedMark is the plain STATUS-cell suffix for an Unfolded Task set — an
// annotation that rides beside the status without changing it, the same shape
// as the Verification mark.
const UnfoldedMark = "unfolded"
