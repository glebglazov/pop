package workload

import (
	"fmt"
	"os"
	"path/filepath"
)

const issuesSubdir = "thoughts/issues"

// Discovery holds non-recursive scan results beneath a definition path.
type Discovery struct {
	Manifests   map[string]string // Issue-set id -> absolute path to index.json
	IssueDirErr error             // non-nil when thoughts/issues exists but is unreadable
}

// CanonicalDefinitionPath returns the canonical exact definition directory.
func CanonicalDefinitionPath(path string) (string, error) {
	return CanonicalDefinitionPathWith(defaultDeps, path)
}

// CanonicalDefinitionPathWith canonicalizes a definition path using provided dependencies.
func CanonicalDefinitionPathWith(d *Deps, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(abs)
	resolved, err := d.FS.EvalSymlinks(clean)
	if err != nil {
		resolved = clean
	}
	return resolved, nil
}

// Discover scans thoughts/issues/*/index.json non-recursively.
func Discover(defPath string) (*Discovery, error) {
	return DiscoverWith(defaultDeps, defPath)
}

// DiscoverWith scans using provided dependencies.
func DiscoverWith(d *Deps, defPath string) (*Discovery, error) {
	result := &Discovery{
		Manifests: make(map[string]string),
	}

	issueDir := filepath.Join(defPath, issuesSubdir)
	if err := scanManifests(d, issueDir, result); err != nil {
		result.IssueDirErr = err
	}

	return result, nil
}

func scanManifests(d *Deps, issueDir string, result *Discovery) error {
	info, err := d.FS.Stat(issueDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", issuesSubdir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", issuesSubdir)
	}

	entries, err := d.FS.ReadDir(issueDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", issuesSubdir, err)
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		manifestPath := filepath.Join(issueDir, ent.Name(), "index.json")
		if _, err := d.FS.Stat(manifestPath); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("read manifest %s: %w", ent.Name(), err)
		}
		result.Manifests[ent.Name()] = manifestPath
	}
	return nil
}
