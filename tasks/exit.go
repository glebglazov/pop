package tasks

import "fmt"

const (
	ExitSuccess     = 0
	ExitOperational = 1
	ExitNoRunnable  = 2
	ExitSetup       = 3
	// ExitQuotaPaused marks a drain that stopped because an agent's quota ran
	// out. It is distinct from ExitSuccess so a supervisor can tell a quota
	// pause (task still Open, partial changes preserved) apart from a clean
	// completion without parsing human output. The value follows sysexits.h
	// EX_TEMPFAIL: the condition is temporary and the set is worth retrying.
	ExitQuotaPaused = 75
	ExitInterrupted = 130
)

// ExitError carries a task execution exit code.
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
