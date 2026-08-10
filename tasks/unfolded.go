package tasks

// Unfolded reports whether a Task set is unfolded: its work is finished but
// pop is still holding a checkout it will tear down — a managed (provisioned)
// Worktree binding, and DONE or Awaiting-approval. It is exactly the foldable
// condition (FoldEligibleStatus plus a provisioned binding), named so a read
// surface can show it (ADR-0197). An adopted binding is not unfolded: there is
// nothing to fold. Derived at read time from fields already loaded for the
// worktree column; never persisted.
func Unfolded(bound, provisioned bool, status TaskSetStatus) bool {
	return bound && provisioned && FoldEligibleStatus(status)
}

// UnfoldedMark is the plain STATUS-cell suffix for an Unfolded Task set — an
// annotation that rides beside the status without changing it, the same shape
// as the Verification mark.
const UnfoldedMark = "unfolded"
