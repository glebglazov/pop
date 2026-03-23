package ui

import (
	"path/filepath"
	"regexp"
)

// LastNSegments returns the last n segments of a path joined with "/".
// For n=2 and path="/a/b/c/d", returns "c/d".
// For n=1, equivalent to filepath.Base.
// For n<=0, returns filepath.Base.
func LastNSegments(path string, n int) string {
	if n <= 1 {
		return filepath.Base(path)
	}
	result := filepath.Base(path)
	dir := filepath.Dir(path)
	for i := 1; i < n; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		result = filepath.Base(dir) + "/" + result
		dir = parent
	}
	return result
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\[[\d;]*m`)

// StripANSI removes ANSI escape codes from a string
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}
