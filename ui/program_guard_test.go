package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// programChokepoint is the one file allowed to start a Bubble Tea program, and
// startProgram is the call that starts one. The call is spelled in halves so
// that this guard does not report itself.
const (
	programChokepoint = "ui/program.go"
	startProgram      = "tea.NewProgram" + "("
)

// Every pop TUI resolves the Terminal appearance and follows it live because it
// starts through RunProgram (ADR-0230). A program started any other way holds
// whatever palette it was built with and never hears the terminal change its
// theme — a symptomless bug, which is why the rule is a guard rather than a
// direction. The chokepoint's own call is the only one exempt.
func TestOnlyTheChokepointStartsATeaProgram(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name != "." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == programChokepoint {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), startProgram) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("these files start a tea program outside the chokepoint — run them through ui.RunProgram instead (ADR-0230):\n%s", strings.Join(offenders, "\n"))
	}
}
