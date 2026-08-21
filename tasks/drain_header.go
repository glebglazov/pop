package tasks

import (
	"io"
	"path/filepath"
)

// DrainHeaderBinding names the Worktree binding kind a whole-set drain runs
// under, as the Drain header states it.
type DrainHeaderBinding string

const (
	// DrainHeaderManaged is a binding whose checkout pop provisioned.
	DrainHeaderManaged DrainHeaderBinding = "managed"
	// DrainHeaderAdopted is a binding whose checkout pop merely adopted.
	DrainHeaderAdopted DrainHeaderBinding = "adopted"
	// DrainHeaderUnbound is a drain with no Worktree binding at all — a runtime
	// override, which routes by path rather than through the binding store.
	DrainHeaderUnbound DrainHeaderBinding = "unbound"
)

// DrainHeader carries what the Drain header states that the executor cannot
// resolve on its own: the kind of Worktree binding the drain runs under, and
// whether this run is the one that recorded a Default binding. Drain routing
// (tasks/implement) knows both and fills this in; the executor prints it.
type DrainHeader struct {
	Binding DrainHeaderBinding
	// RecordedDefaultBinding reports that this run bound the current checkout to
	// an otherwise-unplaced set, so the header can announce the set becoming
	// sticky to where it first ran.
	RecordedDefaultBinding bool
}

// renderDrainHeader prints the opening lines of a whole-set drain: the set, the
// resolved Runtime path, and the binding kind, unconditionally — where a drain
// runs is always stated, not only when it surprises. The invocation directory is
// named only when it differs from the Runtime path, and a just-recorded Default
// binding is announced at the moment it happens.
func renderDrainHeader(w io.Writer, setID, runtimePath, invokedFrom string, header DrainHeader) {
	binding := header.Binding
	if binding == "" {
		binding = DrainHeaderUnbound
	}
	o := outputFor(w)
	o.line(ansiBold, "drain %s at %s (%s)", setID, runtimePath, binding)
	if invokedFrom != "" && !sameDirectory(invokedFrom, runtimePath) {
		o.line(ansiDim, "invoked from %s", invokedFrom)
	}
	if header.RecordedDefaultBinding {
		o.line(ansiDim, "binding recorded to current checkout")
	}
}

// sameDirectory reports whether two paths name the same directory. The Runtime
// path is canonicalized on resolution while an invocation directory is not, so a
// plain string compare would call a symlinked cwd a different place and make the
// header's "invoked from" line fire on every run under one.
func sameDirectory(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// renderCurrentCheckoutLine prints the single-task file run's sibling of the
// Drain header. Such a run claims no drain and never routes to a binding — it
// always runs where it was invoked — so it states just that.
func renderCurrentCheckoutLine(w io.Writer, runtimePath string) {
	outputFor(w).line(ansiBold, "running in current checkout %s", runtimePath)
}
