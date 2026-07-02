package tasks

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// ExportOptions selects one or more Task sets to export and where to write the
// archive.
type ExportOptions struct {
	ResolveInput ResolveInput
	TaskSetIDs   []string
	OutputPath   string
	Now          time.Time
}

// ExportResult is the outcome of exporting one or more Task sets into a single
// archive.
type ExportResult struct {
	TaskSetIDs []string
	Path       string
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
	if len(input.TaskSetIDs) == 0 {
		return nil, exitErr(ExitSetup, "expected at least one bare Task set identifier")
	}

	// Normalize and dedupe, preserving first-seen order.
	seen := make(map[string]bool, len(input.TaskSetIDs))
	taskSetIDs := make([]string, 0, len(input.TaskSetIDs))
	for _, raw := range input.TaskSetIDs {
		id, err := normalizeExportTaskSetID(raw)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		taskSetIDs = append(taskSetIDs, id)
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

	// Validate every requested set up front — the export is atomic, so a single
	// Missing id fails the whole request and writes nothing.
	sets := make([]exportSet, 0, len(taskSetIDs))
	var missing []string
	for _, tid := range taskSetIDs {
		setDir := filepath.Join(id.TasksDir, tid)
		info, statErr := d.FS.Stat(setDir)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				missing = append(missing, tid)
				continue
			}
			return nil, exitErr(ExitOperational, "inspect Task set %q: %v", tid, statErr)
		}
		if !info.IsDir() {
			return nil, exitErr(ExitSetup, "Task set %q is not a directory", tid)
		}
		sets = append(sets, exportSet{id: tid, dir: setDir})
	}
	if len(missing) > 0 {
		valid := readTaskSetIDs(d, id.TasksDir)
		if len(valid) == 0 {
			return nil, exitErr(ExitSetup, "unknown Task set(s) %s (no Task sets in storage)", quoteJoin(missing))
		}
		return nil, exitErr(ExitSetup, "unknown Task set(s) %s; valid identifiers: %s", quoteJoin(missing), strings.Join(valid, ", "))
	}

	outputPath := input.OutputPath
	if outputPath == "" {
		outputPath = defaultExportOutput(taskSetIDs, input.Now)
	}
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, exitErr(ExitSetup, "resolve output path: %v", err)
	}

	if err := writeTaskSetsArchive(sets, absOutput); err != nil {
		return nil, err
	}

	return &ExportResult{
		TaskSetIDs: taskSetIDs,
		Path:       absOutput,
	}, nil
}

// defaultExportOutput picks the default archive filename: <id>.tar.gz for a
// single set (unchanged), pop-tasks-<YYYY-MM-DD-HHMM>.tar.gz for many.
func defaultExportOutput(taskSetIDs []string, now time.Time) string {
	if len(taskSetIDs) == 1 {
		return taskSetIDs[0] + ".tar.gz"
	}
	if now.IsZero() {
		now = time.Now()
	}
	return "pop-tasks-" + now.Format("2006-01-02-1504") + ".tar.gz"
}

func quoteJoin(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf("%q", id)
	}
	return strings.Join(quoted, ", ")
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
		manifestPath := filepath.Join(defPath, taskSetID, "index.json")
		entry.TaskSets = append(entry.TaskSets, registeredTaskSetFromManifest(d, taskSetID, manifestPath))
		return nil
	})
}

// exportSet pairs a Task set identifier with its on-disk directory for archiving.
type exportSet struct {
	id  string
	dir string
}

// writeTaskSetArchive writes a single Task set — the N=1 case of
// writeTaskSetsArchive, kept as a convenience for single-set callers.
func writeTaskSetArchive(srcDir, taskSetID, outputPath string) error {
	return writeTaskSetsArchive([]exportSet{{id: taskSetID, dir: srcDir}}, outputPath)
}

// writeTaskSetsArchive packs one or more Task sets into a single tar.gz, each
// under its own top-level directory named for its identifier.
func writeTaskSetsArchive(sets []exportSet, outputPath string) error {
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

	for _, set := range sets {
		if err := writeTaskSetTree(tw, set.dir, set.id); err != nil {
			return err
		}
	}
	return nil
}

// writeTaskSetTree walks one set directory into the tar writer, rooting every
// entry under the set's identifier.
func writeTaskSetTree(tw *tar.Writer, srcDir, taskSetID string) error {
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
