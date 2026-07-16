package tasks

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// ImportedTaskSet is one Task set installed by an import.
type ImportedTaskSet struct {
	TaskSetID string
	Path      string
}

// ImportResult is the outcome of importing an archive of one or more Task sets.
type ImportResult struct {
	Sets []ImportedTaskSet
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

	// Extract every top-level set to the temp location. The per-entry traversal
	// and absolute-path guards apply to every entry, so a bad path in any set
	// rejects the whole archive before anything is installed.
	archiveIDs, err := extractTaskSetsArchive(archivePath, tempDir)
	if err != nil {
		return nil, err
	}

	// --as renames the imported set, so it is meaningful only for a single-set
	// archive (ADR-0080).
	asID := strings.TrimSpace(input.AsID)
	if asID != "" && len(archiveIDs) > 1 {
		return nil, exitErr(ExitSetup, "--as renames a single imported Task set, but the archive holds %d sets (%s); import without --as to keep their own names", len(archiveIDs), quoteJoin(archiveIDs))
	}

	// Validate every set against the task contract. Any Malformed set rejects
	// the entire archive; nothing is written.
	var malformed []string
	for _, archiveID := range archiveIDs {
		manifestPath := filepath.Join(tempDir, archiveID, "index.json")
		manifest := LoadManifest(d, archiveID, manifestPath)
		if manifest.Valid {
			continue
		}
		if len(manifest.Errors) == 0 {
			malformed = append(malformed, fmt.Sprintf("%q", archiveID))
		} else {
			malformed = append(malformed, fmt.Sprintf("%q (%s)", archiveID, strings.Join(manifest.Errors, "; ")))
		}
	}
	if len(malformed) > 0 {
		return nil, exitErr(ExitSetup, "imported Task set(s) malformed: %s", strings.Join(malformed, "; "))
	}

	// Resolve every target identifier up front. Disambiguation may rename a set
	// to a dated identifier; a still-unresolved collision rejects everything
	// before any install. claimed guards against two sets in the same archive
	// disambiguating onto the same target.
	type importPlan struct {
		archiveID string
		targetID  string
	}
	claimed := make(map[string]bool, len(archiveIDs))
	plans := make([]importPlan, 0, len(archiveIDs))
	for _, archiveID := range archiveIDs {
		targetID, err := resolveImportTaskSetID(d, id.TasksDir, asID, archiveID, now, claimed)
		if err != nil {
			return nil, err
		}
		claimed[targetID] = true
		plans = append(plans, importPlan{archiveID: archiveID, targetID: targetID})
	}

	// Every set is well-formed and every target is free — commit all installs.
	for _, p := range plans {
		src := filepath.Join(tempDir, p.archiveID)
		dst := filepath.Join(id.TasksDir, p.targetID)
		if err := moveTree(d, src, dst); err != nil {
			return nil, exitErr(ExitOperational, "install Task set %q: %v", p.targetID, err)
		}
	}

	canonDefPath, err := CanonicalDefinitionPathWith(d, id.TasksDir)
	if err != nil {
		return nil, exitErr(ExitSetup, "canonicalize task storage: %v", err)
	}
	statePath := StatePathFor(canonDefPath)

	// Register every installed set at priority 0, appended in identifier order.
	sort.Slice(plans, func(i, j int) bool { return plans[i].targetID < plans[j].targetID })
	orderedIDs := make([]string, len(plans))
	for i, p := range plans {
		orderedIDs[i] = p.targetID
	}
	if err := registerImportedTaskSets(d, statePath, canonDefPath, orderedIDs); err != nil {
		return nil, err
	}

	sets := make([]ImportedTaskSet, 0, len(plans))
	for _, p := range plans {
		absDst, err := filepath.Abs(filepath.Join(id.TasksDir, p.targetID))
		if err != nil {
			return nil, exitErr(ExitOperational, "resolve installed path: %v", err)
		}
		sets = append(sets, ImportedTaskSet{TaskSetID: p.targetID, Path: absDst})
	}

	return &ImportResult{Sets: sets}, nil
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

// resolveImportTaskSetID picks the on-disk identifier for an imported set,
// reusing the task-set-creation disambiguation ladder: prefer the requested
// name (the archive directory name by default, or the --as override), then
// prepend today's YYYY-MM-DD, then YYYY-MM-DD-HHMM. An identifier is taken when
// a directory already exists in storage or another set in the same import has
// claimed it. A collision that survives the HHMM form fails the whole import.
func resolveImportTaskSetID(d *Deps, tasksDir, asID, archiveID string, now time.Time, claimed map[string]bool) (string, error) {
	base := archiveID
	slug := taskSetSlug(archiveID)
	if asID != "" {
		base = asID
		slug = asID
		if !hasChronologicalPrefix(asID) {
			base = now.Format("2006-01-02") + "-" + asID
		}
	}

	for _, candidate := range []string{
		base,
		now.Format("2006-01-02") + "-" + slug,
		now.Format("2006-01-02-1504") + "-" + slug,
	} {
		if !idTaken(d, tasksDir, claimed, candidate) {
			return candidate, nil
		}
	}

	return "", exitErr(ExitSetup, "Task set %q already exists in storage (and dated fallbacks collide too)", base)
}

func idTaken(d *Deps, tasksDir string, claimed map[string]bool, id string) bool {
	return claimed[id] || taskSetDirExists(d, tasksDir, id)
}

// registerImportedTaskSets appends the installed sets to Task state at priority
// 0 in the order given, in a single state update. A set already registered for
// this definition path is left untouched.
func registerImportedTaskSets(d *Deps, statePath, defPath string, taskSetIDs []string) error {
	return UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		registered := state.RegisteredIDs(defPath)
		entry := state.Entry(defPath)
		for _, taskSetID := range taskSetIDs {
			if _, ok := registered[taskSetID]; ok {
				continue
			}
			entry.TaskSets = append(entry.TaskSets, newRegisteredTaskSet(taskSetID))
			registered[taskSetID] = len(entry.TaskSets) - 1
		}
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

// extractTaskSetsArchive extracts a transfer archive to destDir and returns the
// names of its top-level set directories in first-seen order. The archive shape
// is "one or more top-level directories" (a single-set archive is the N=1
// case); every entry is guarded against path traversal and absolute paths.
func extractTaskSetsArchive(archivePath, destDir string) ([]string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, exitErr(ExitSetup, "archive not found: %s", archivePath)
		}
		return nil, exitErr(ExitOperational, "open archive %q: %v", archivePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, exitErr(ExitSetup, "read archive %q: %v", archivePath, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	seen := make(map[string]bool)
	var topLevels []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, exitErr(ExitSetup, "read archive %q: %v", archivePath, err)
		}

		name, err := validateArchiveEntryName(header.Name)
		if err != nil {
			return nil, err
		}
		if name == "" {
			continue
		}

		root := strings.SplitN(name, "/", 2)[0]
		if !seen[root] {
			seen[root] = true
			topLevels = append(topLevels, root)
		}

		target := filepath.Join(destDir, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return nil, exitErr(ExitOperational, "extract archive: %v", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, exitErr(ExitOperational, "extract archive: %v", err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return nil, exitErr(ExitOperational, "extract archive: %v", err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return nil, exitErr(ExitOperational, "extract archive: %v", err)
			}
			if err := out.Close(); err != nil {
				return nil, exitErr(ExitOperational, "extract archive: %v", err)
			}
		default:
			return nil, exitErr(ExitSetup, "unsupported archive entry %q", header.Name)
		}
	}

	if len(topLevels) == 0 {
		return nil, exitErr(ExitSetup, "archive is empty")
	}
	return topLevels, nil
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
