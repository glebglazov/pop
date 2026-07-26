package tmux

// recordingRunner is a runner fake for the module's own verb tests. It
// records every argument vector it is handed and returns a canned response.
// This is the one place argument construction is asserted — consumers use
// the stateful fake in tmuxtest and never see argument arrays.
type recordingRunner struct {
	calls       [][]string
	attachCalls [][]string
	out         string
	err         error
}

func (r *recordingRunner) output(args ...string) (string, error) {
	r.calls = append(r.calls, args)
	return r.out, r.err
}

func (r *recordingRunner) attach(args ...string) error {
	r.attachCalls = append(r.attachCalls, args)
	return r.err
}
