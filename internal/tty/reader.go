package tty

import (
	"bufio"
	"io"
	"os"
)

// TerminalFd reports the file descriptor behind r when r is a real terminal.
// Callers use it both to place an attended child in that terminal's foreground
// group and to decide whether job control applies at all: a pipe or a test's
// in-memory reader has no foreground to own.
func TerminalFd(r io.Reader) (int, bool) {
	f, ok := r.(*os.File)
	if !ok {
		return 0, false
	}
	info, err := f.Stat()
	if err != nil {
		return 0, false
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return 0, false
	}
	return int(f.Fd()), true
}

// Reader is a buffered line reader that owns the terminal before it reads it.
//
// Buffering alone is not enough to be able to read: Pop hands the terminal
// foreground to each attended agent and takes it back when the agent exits, but
// that hand-back is not a guarantee — any descendant the agent left behind can
// move the foreground again afterwards. A read from a background process group
// draws SIGTTIN and the kernel stops Pop, so the human is left staring at a
// fully rendered menu whose prompt never accepts input. Pairing the buffer with
// its terminal lets every prompt re-assert ownership immediately before reading.
//
// One Reader must be shared by every prompt in a run: a fresh bufio.Reader
// buffers ahead on its first read, so a per-prompt reader would swallow input
// queued for later prompts.
type Reader struct {
	br    *bufio.Reader
	ttyFd int
	isTTY bool
}

// NewReader wraps in, remembering its terminal fd when in is one. A
// non-terminal input skips the job-control dance entirely.
func NewReader(in io.Reader) *Reader {
	if in == nil {
		in = os.Stdin
	}
	r := &Reader{br: bufio.NewReader(in)}
	r.ttyFd, r.isTTY = TerminalFd(in)
	return r
}

// ReadLine reads one line, first making this process's group the owner of the
// terminal foreground and then reading with SIGTTIN/SIGTTOU blocked. warn — nil
// is allowed — receives one human-facing line per surprise: a foreground that
// had to be taken, or one that could not be.
//
// The two steps answer different failures. The claim makes the ordinary case
// work: a foreground left elsewhere is taken back and the read proceeds. The
// block is the backstop for a claim that could not be made — the kernel then
// fails the read with EIO, a named error, instead of stopping the process with
// no diagnostic at all.
func (r *Reader) ReadLine(warn func(format string, args ...any)) (string, error) {
	if r == nil || r.br == nil {
		return "", io.EOF
	}
	if !r.isTTY {
		return r.br.ReadString('\n')
	}
	if warn == nil {
		warn = func(string, ...any) {}
	}

	claim := ClaimForeground(r.ttyFd)
	switch {
	case claim.Owned && claim.Taken:
		warn("Terminal foreground was held by process group %d; took it back to prompt.", claim.Holder)
	case !claim.Owned:
		warn("Could not take the terminal foreground to prompt: %v", claim.Err)
	}

	var answer string
	var readErr error
	read := func() error {
		answer, readErr = r.br.ReadString('\n')
		return nil
	}
	if err := GuardRead(read); err != nil {
		warn("Could not block terminal job-control signals while prompting: %v", err)
		_ = read()
	}
	return answer, readErr
}
