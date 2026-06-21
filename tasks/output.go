package tasks

import (
	"fmt"
	"io"
	"os"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

// output styles pop-owned text without changing streamed agent output.
type output struct {
	io.Writer
	color bool
}

func outputFor(w io.Writer) *output {
	if out, ok := w.(*output); ok {
		return out
	}
	return &output{Writer: w, color: colorEnabled(writerIsTerminal(w))}
}

func colorEnabled(terminal bool) bool {
	return terminal && os.Getenv("NO_COLOR") == ""
}

func writerIsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func (o *output) styled(style, text string) string {
	if !o.color {
		return text
	}
	return style + text + ansiReset
}

func (o *output) line(style, format string, args ...any) {
	fmt.Fprintln(o, o.styled(style, fmt.Sprintf(format, args...)))
}

func statusStyle(status TaskSetStatus) string {
	switch status {
	case StatusDone:
		return ansiGreen
	case StatusFailed, StatusMalformed:
		return ansiRed
	case StatusReady:
		return ansiCyan
	case StatusBlocked, StatusUnverified, StatusDeferred:
		return ansiYellow
	default:
		return ansiDim
	}
}
