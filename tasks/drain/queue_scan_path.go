package drain

import "github.com/glebglazov/pop/project"

// ScanDefinitionPath returns the Task-set definition path a picker project
// resolves to. It is the one field of a project scan a caller outside the drain
// pipeline needs; the scan itself carries the pipeline's own working state.
func ScanDefinitionPath(d *Deps, p project.ExpandedProject) (string, error) {
	scan, err := resolveScan(d, p)
	return scan.DefinitionPath, err
}
