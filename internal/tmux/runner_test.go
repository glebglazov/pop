package tmux

// recordingRunner is a runner fake for the module's own verb tests. It
// records every argument vector it is handed and returns a canned response.
// This is the one place argument construction is asserted — consumers use
// the stateful fake in tmuxtest and never see argument arrays.
type recordingRunner struct {
	calls       [][]string
	attachCalls [][]string
	inputCalls  []inputCall
	out         string
	err         error
	// responses, when non-nil, are consumed one per output() call before
	// falling back to out/err — for verbs that probe then act (e.g. base
	// config's list-sessions then new-session).
	responses []runnerResponse
}

// runnerResponse is one canned output() result for a scripted recordingRunner.
type runnerResponse struct {
	out string
	err error
}

// inputCall records one input() invocation: the text streamed to tmux's
// stdin alongside its argument vector.
type inputCall struct {
	text string
	args []string
}

func (r *recordingRunner) output(args ...string) (string, error) {
	r.calls = append(r.calls, args)
	if len(r.responses) > 0 {
		step := r.responses[0]
		r.responses = r.responses[1:]
		return step.out, step.err
	}
	return r.out, r.err
}

func (r *recordingRunner) attach(args ...string) error {
	r.attachCalls = append(r.attachCalls, args)
	return r.err
}

func (r *recordingRunner) input(text string, args ...string) error {
	r.inputCalls = append(r.inputCalls, inputCall{text: text, args: args})
	return r.err
}
