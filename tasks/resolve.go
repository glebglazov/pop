package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// ResolveInput selects how a task command locates project and definition paths.
type ResolveInput struct {
	ProjectName        string
	Path               string
	DefinitionOverride string
	RuntimeOverride    string
	CWD                string
}

// ResolvedPaths holds canonical project and task-definition paths.
type ResolvedPaths struct {
	ProjectPath    string
	DefinitionPath string
}

// ResolvePaths determines project and definition paths from ResolveInput.
func ResolvePaths(input ResolveInput) (*ResolvedPaths, error) {
	return ResolvePathsWith(defaultDeps, project.DefaultDeps(), config.Load, input)
}

// ResolvePathsWith resolves paths using injected dependencies.
func ResolvePathsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput) (*ResolvedPaths, error) {
	cwd := input.CWD
	if cwd == "" {
		var err error
		cwd, err = d.FS.Getwd()
		if err != nil {
			return nil, err
		}
	}

	var projectPath string
	var err error

	switch {
	case input.ProjectName != "":
		projectPath, err = resolveByProjectName(pd, loadConfig, input.ProjectName)
	case input.Path != "":
		projectPath, err = NormalizeProjectPathWith(d, input.Path)
	default:
		projectPath, err = NormalizeProjectPathWith(d, cwd)
	}
	if err != nil {
		return nil, err
	}

	defPath, err := resolveDefinitionPath(d, projectPath, input.DefinitionOverride)
	if err != nil {
		return nil, err
	}

	return &ResolvedPaths{
		ProjectPath:    projectPath,
		DefinitionPath: defPath,
	}, nil
}

func resolveByProjectName(pd *project.Deps, loadConfig func(string) (*config.Config, error), name string) (string, error) {
	cfg, err := loadConfig(config.DefaultConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("unknown project %q (no pop config)", name)
		}
		return "", fmt.Errorf("load config: %w", err)
	}

	projects, err := ListPickerProjectsWith(pd, cfg)
	if err != nil {
		return "", err
	}

	return MatchPickerProject(name, projects)
}

// MatchPickerProject resolves an exact picker-visible project name.
func MatchPickerProject(name string, projects []project.ExpandedProject) (string, error) {
	var matches []project.ExpandedProject
	for _, p := range projects {
		if p.Name == name {
			matches = append(matches, p)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown project %q", name)
	case 1:
		return matches[0].Path, nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "ambiguous project %q; candidates:", name)
		for _, m := range matches {
			fmt.Fprintf(&b, "\n  %s (%s)", m.Name, m.Path)
		}
		return "", fmt.Errorf("%s", b.String())
	}
}

// resolveDefinitionPath resolves the task definition directory: the repository's
// Task storage tasks directory, derived per ADR 0012 from the project's git
// common directory. An explicit override names that tasks directory directly (testing).
func resolveDefinitionPath(d *Deps, projectPath, override string) (string, error) {
	if override != "" {
		return CanonicalDefinitionPathWith(d, override)
	}
	id, err := ResolveRepositoryIdentity(d, projectPath)
	if err != nil {
		return "", err
	}
	// Canonicalize so the returned path matches the state key RefreshWith derives
	// (it canonicalizes the definition path), keeping state lookups keyed by
	// resolved.DefinitionPath consistent across invocations.
	return CanonicalDefinitionPathWith(d, id.TasksDir)
}

// NormalizeProjectPath canonicalizes path and normalizes git subdirectories to checkout roots.
func NormalizeProjectPath(path string) (string, error) {
	return NormalizeProjectPathWith(defaultDeps, path)
}

// NormalizeProjectPathWith canonicalizes path using injected dependencies.
func NormalizeProjectPathWith(d *Deps, path string) (string, error) {
	canon, err := canonicalAbsPath(d, path)
	if err != nil {
		return "", err
	}

	topLevel, err := d.Git.CommandInDir(canon, "rev-parse", "--show-toplevel")
	if err == nil && topLevel != "" {
		return canonicalAbsPath(d, topLevel)
	}
	return canon, nil
}

// NormalizeRuntimePath canonicalizes a path to its git checkout root.
func NormalizeRuntimePath(path string) (string, error) {
	return NormalizeRuntimePathWith(defaultDeps, path)
}

// NormalizeRuntimePathWith canonicalizes a runtime path to its git checkout root.
// Non-git paths are rejected.
func NormalizeRuntimePathWith(d *Deps, path string) (string, error) {
	canon, err := canonicalAbsPath(d, path)
	if err != nil {
		return "", err
	}

	topLevel, err := d.Git.CommandInDir(canon, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(topLevel) == "" {
		return "", fmt.Errorf("runtime path %q is not a git checkout", path)
	}
	return canonicalAbsPath(d, topLevel)
}

// ResolveRuntimePath returns the canonical runtime checkout root.
func ResolveRuntimePath(projectPath, override string) (string, error) {
	return ResolveRuntimePathWith(defaultDeps, projectPath, override)
}

// ResolveRuntimePathWith resolves the runtime checkout root from project path and override.
func ResolveRuntimePathWith(d *Deps, projectPath, override string) (string, error) {
	if override != "" {
		return NormalizeRuntimePathWith(d, override)
	}
	return NormalizeRuntimePathWith(d, projectPath)
}

func canonicalAbsPath(d *Deps, path string) (string, error) {
	expanded := expandHome(d, path)
	abs, err := filepath.Abs(expanded)
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

func expandHome(d *Deps, path string) string {
	if path == "~" {
		home, err := d.FS.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := d.FS.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
