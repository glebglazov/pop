package debug

import (
	"fmt"
	"log"
	"os"
)

var logger *log.Logger
var file *os.File

// Init initializes the debug logger. If POP_LOG is set, logs are written
// to that file. Otherwise, all log calls are no-ops.
func Init() {
	path := os.Getenv("POP_LOG")
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
	logger.Printf("=== pop debug log started ===")
}

// Log writes a formatted message to the debug log. No-op if logging is disabled.
func Log(format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf(format, args...)
}

// Close flushes and closes the log file.
func Close() {
	if file != nil {
		file.Close()
		file = nil
		logger = nil
	}
}
