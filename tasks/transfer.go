package tasks

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// ExportOptions selects a Task set to export and where to write the archive.
type ExportOptions struct {
	ResolveInput ResolveInput
	TaskSetID    string
	OutputPath   string
}

// ExportResult is the outcome of exporting one Task set.
type ExportResult struct {
	TaskSetID string
	Path      string
}

// ImportOptions selects an archive to import into the current repository.
type ImportOptions struct {
	ResolveInput ResolveInput
	ArchivePath  string
	AsID         string
	Now          time.Time
}

// ImportResult is the outcome of importing one Task set.
type ImportResult struct {
	TaskSetID string
	Path      string
}

// Export creates a tar.gz archive of one on-disk Task set.
func Export(input ExportOptions) (*ExportResult, error) {
	return ExportWith(defaultDeps, project.DefaultDeps(), config.Load, input)
}

// ExportWith exports using injected dependencies.
func ExportWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ExportOptions) (*ExportResult, error) {
	taskSetID, err := normalizeExportTaskSetID(input.TaskSetID)
	if err != nil {
		return nil, err
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, input.ResolveInput)
	if err != nil {
		return nil, err
	}

	id, err := ResolveRepositoryIdentity(d, resolved.ProjectPath)
	if err != nil {
		return nil, err
	}
	if err := EnsureStorage(d, id); err != nil {
		return nil, err
	}

	setDir := filepath.Join(id.TasksDir, taskSetID)
	info, err := d.FS.Stat(setDir)
	if err != nil {
		if os.IsNotExist(err) {
			valid := readTaskSetIDs(d, id.TasksDir)
			if len(valid) == 0 {
				return nil, exitErr(ExitSetup, "unknown Task set %q (no Task sets in storage)", taskSetID)
			}
			return nil, exitErr(ExitSetup, "unknown Task set %q; valid identifiers: %s", taskSetID, strings.Join(valid, ", "))
		}
		return nil, exitErr(ExitOperational, "inspect Task set %q: %v", taskSetID, err)
	}
	if !info.IsDir() {
		return nil, exitErr(ExitSetup, "Task set %q is not a directory", taskSetID)
	}

	outputPath := input.OutputPath
	if outputPath == "" {
		outputPath = taskSetID + ".tar.gz"
	}
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, exitErr(ExitSetup, "resolve output path: %v", err)
	}

	if err := writeTaskSetArchive(setDir, taskSetID, absOutput); err != nil {
		return nil, err
	}

	return &ExportResult{
		TaskSetID: taskSetID,
		Path:      absOutput,
	}, nil
}

// Import installs a Task set export into the current repository's Task storage.
func Import(input ImportOptions) (*ImportResult, error) {
	return ImportWith(defaultDeps, project.DefaultDeps(), config.Load, input)
}

// ImportWith imports using injected dependencies.
func ImportWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ImportOptions) (*ImportResult, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, input.ResolveInput)
	if err != nil {
		return nil, err
	}

	id, err := ResolveRepositoryIdentity(d, resolved.ProjectPath)
	if err != nil {
		return nil, err
	}
	if err := EnsureStorage(d, id); err != nil {
		return nil, err
	}

	archivePath, err := filepath.Abs(input.ArchivePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "resolve archive path: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "pop-task-import-*")
	if err != nil {
		return nil, exitErr(ExitOperational, "create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	archiveID, err := extractTaskSetArchive(archivePath, tempDir)
	if err != nil {
		return nil, err
	}

	extractedDir := filepath.Join(tempDir, archiveID)
	manifest := LoadManifest(d, archiveID, filepath.Join(extractedDir, "index.json"))
	if !manifest.Valid {
		if len(manifest.Errors) == 0 {
			return nil, exitErr(ExitSetup, "imported Task set %q is malformed", archiveID)
		}
		return nil, exitErr(ExitSetup, "imported Task set %q is malformed: %s", archiveID, strings.Join(manifest.Errors, "; "))
	}

	targetID, err := resolveImportTaskSetID(d, id.TasksDir, strings.TrimSpace(input.AsID), archiveID, now)
	if err != nil {
		return nil, err
	}

	dst := filepath.Join(id.TasksDir, targetID)
	if err := moveTree(d, extractedDir, dst); err != nil {
		return nil, exitErr(ExitOperational, "install Task set %q: %v", targetID, err)
	}

	canonDefPath, err := CanonicalDefinitionPathWith(d, id.TasksDir)
	if err != nil {
		return nil, exitErr(ExitSetup, "canonicalize task storage: %v", err)
	}
	statePath := StatePathFor(canonDefPath)
	if err := registerImportedTaskSet(d, statePath, canonDefPath, targetID); err != nil {
		return nil, err
	}

	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, exitErr(ExitOperational, "resolve installed path: %v", err)
	}

	return &ImportResult{
		TaskSetID: targetID,
		Path:      absDst,
	}, nil
}

func normalizeExportTaskSetID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", exitErr(ExitSetup, "expected a bare Task set identifier")
	}
	slash := filepath.ToSlash(raw)
	if filepath.IsAbs(raw) || raw == "~" || strings.HasPrefix(slash, "~/") {
		return "", exitErr(ExitSetup, "invalid Task set identifier %q: absolute paths are not task target references", raw)
	}
	if raw == "." || raw == ".." || strings.HasPrefix(slash, "./") || strings.HasPrefix(slash, "../") {
		return "", exitErr(ExitSetup, "invalid Task set identifier %q: relative paths are not task target references", raw)
	}
	slash = strings.TrimSuffix(slash, "/")
	if strings.Contains(slash, "/") {
		return "", exitErr(ExitSetup, "invalid Task set identifier %q: expected a bare Task set identifier", raw)
	}
	if strings.HasSuffix(slash, ".md") {
		return "", exitErr(ExitSetup, "invalid Task set identifier %q: expected a bare Task set identifier, not a file name", raw)
	}
	return slash, nil
}

func hasChronologicalPrefix(id string) bool {
	return timestampPrefixPattern.MatchString(id)
}

func taskSetDirExists(d *Deps, tasksDir, id string) bool {
	info, err := d.FS.Stat(filepath.Join(tasksDir, id))
	return err == nil && info.IsDir()
}

func resolveImportTaskSetID(d *Deps, tasksDir, asID, archiveID string, now time.Time) (string, error) {
	candidate := archiveID
	autoPrefixed := false
	slug := strings.TrimSpace(asID)

	if slug != "" {
		candidate = slug
		if !hasChronologicalPrefix(slug) {
			candidate = now.Format("2006-01-02") + "-" + slug
			autoPrefixed = true
		}
	}

	if !taskSetDirExists(d, tasksDir, candidate) {
		return candidate, nil
	}

	if autoPrefixed {
		disambiguated := now.Format("2006-01-02-1504") + "-" + slug
		if !taskSetDirExists(d, tasksDir, disambiguated) {
			return disambiguated, nil
		}
		candidate = disambiguated
	}

	return "", exitErr(ExitSetup, "Task set %q already exists in storage", candidate)
}

func registerImportedTaskSet(d *Deps, statePath, defPath, taskSetID string) error {
	return UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		registered := state.RegisteredIDs(defPath)
		if _, ok := registered[taskSetID]; ok {
			return nil
		}
		entry := state.Entry(defPath)
		entry.TaskSets = append(entry.TaskSets, RegisteredTaskSet{
			ID:       taskSetID,
			Priority: 0,
		})
		return nil
	})
}

func writeTaskSetArchive(srcDir, taskSetID, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil && filepath.Dir(outputPath) != "." {
		return exitErr(ExitOperational, "create output directory: %v", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return exitErr(ExitOperational, "create archive %q: %v", outputPath, err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(taskSetID, rel))
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := io.Copy(tw, file); err != nil {
			return err
		}
		return nil
	})
}

func extractTaskSetArchive(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", exitErr(ExitSetup, "archive not found: %s", archivePath)
		}
		return "", exitErr(ExitOperational, "open archive %q: %v", archivePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", exitErr(ExitSetup, "read archive %q: %v", archivePath, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var topLevel string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", exitErr(ExitSetup, "read archive %q: %v", archivePath, err)
		}

		name, err := validateArchiveEntryName(header.Name)
		if err != nil {
			return "", err
		}
		if name == "" {
			continue
		}

		parts := strings.Split(name, "/")
		root := parts[0]
		if topLevel == "" {
			topLevel = root
		} else if topLevel != root {
			return "", exitErr(ExitSetup, "archive must contain exactly one top-level directory, found %q and %q", topLevel, root)
		}

		target := filepath.Join(destDir, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return "", exitErr(ExitOperational, "extract archive: %v", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", exitErr(ExitOperational, "extract archive: %v", err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return "", exitErr(ExitOperational, "extract archive: %v", err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", exitErr(ExitOperational, "extract archive: %v", err)
			}
			if err := out.Close(); err != nil {
				return "", exitErr(ExitOperational, "extract archive: %v", err)
			}
		default:
			return "", exitErr(ExitSetup, "unsupported archive entry %q", header.Name)
		}
	}

	if topLevel == "" {
		return "", exitErr(ExitSetup, "archive is empty")
	}
	return topLevel, nil
}

func validateArchiveEntryName(name string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean == "" {
		return "", nil
	}
	if strings.HasPrefix(clean, "/") || filepath.IsAbs(clean) {
		return "", exitErr(ExitSetup, "archive entry %q uses an absolute path", name)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", exitErr(ExitSetup, "archive entry %q escapes the archive root", name)
	}
	return clean, nil
}
