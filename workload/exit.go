package workload

import "fmt"

const (
	ExitSuccess       = 0
	ExitOperational   = 1
	ExitNoRunnable    = 2
	ExitSetup         = 3
	ExitInterrupted   = 130
)

// ExitError carries a workload execution exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit status %d", e.Code)
}

func exitErr(code int, format string, args ...any) *ExitError {
	return &ExitError{Code: code, Err: fmt.Errorf(format, args...)}
}
