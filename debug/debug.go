package debug

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

var logger *log.Logger
var file *os.File

var errorLogger *log.Logger
var errorFile *os.File

// defaultLogPath is set at compile time via -ldflags when building with DEBUG=true.
// When set, logging is enabled by default without needing POP_LOG.
var defaultLogPath string

// defaultErrorLogPath returns ~/.local/share/pop/pop.log, respecting XDG_DATA_HOME.
func defaultErrorLogPath() string {
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "pop.log")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "pop", "pop.log")
}

// Init initializes both the always-on error logger and the opt-in debug
// logger. The error logger writes to ~/.local/share/pop/pop.log (or
// $XDG_DATA_HOME/pop/pop.log). The debug logger is only active when
// POP_LOG is set or the binary was built with DEBUG=true.
func Init() {
	// Always-on error logger
	if errPath := defaultErrorLogPath(); errPath != "" {
		if err := os.MkdirAll(filepath.Dir(errPath), 0o755); err == nil {
			if f, err := os.OpenFile(errPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				errorFile = f
				errorLogger = log.New(f, "ERROR ", log.Ldate|log.Ltime|log.Lmicroseconds)
			}
		}
	}

	// Opt-in debug logger
	path := os.Getenv("POP_LOG")
	if path == "" {
		path = defaultLogPath
	}
	if path == "" {
		return
	}

	var err error
	file, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "debug: failed to open log file %s: %v\n", path, err)
		return
	}

	logger = log.New(file, "", log.Ltime|log.Lmicroseconds)
}

// Log writes a formatted message to the debug log. No-op if logging is disabled.
func Log(format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf(format, args...)
}

// Error writes a formatted error message to the error log. Always active
// (unlike Log which requires POP_LOG). Use for errors that should be
// persisted for post-crash forensics.
func Error(format string, args ...any) {
	if errorLogger == nil {
		return
	}
	errorLogger.Printf(format, args...)
}

// Close flushes and closes all log files.
func Close() {
	if file != nil {
		file.Close()
		file = nil
		logger = nil
	}
	if errorFile != nil {
		errorFile.Close()
		errorFile = nil
		errorLogger = nil
	}
}
