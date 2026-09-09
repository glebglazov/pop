package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

const (
	caseManifestName = "case.json"
	acceptanceName   = "acceptance.md"
)

// The harness roots every command reads and writes under. They are defaults
// rather than fixed paths so a caller can point a whole run at another tree.
var (
	defaultCasesRoot   = filepath.Join("eval", "cases")
	defaultArmsRoot    = filepath.Join("eval", "arms")
	defaultGradersRoot = filepath.Join("eval", "graders")
	defaultConfigPath  = filepath.Join("eval", "config.toml")
	defaultWorkRoot    = evalWorkRootWith(os.Getenv, os.UserHomeDir)
	defaultResultsRoot = filepath.Join("eval", "results")
)

func evalWorkRootWith(getenv func(string) string, userHomeDir func() (string, error)) string {
	if stateHome := getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "pop", "eval", "work")
	}
	home, err := userHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", "eval", "work")
	}
	return filepath.Join(home, ".local", "state", "pop", "eval", "work")
}

// A Case, an Arm and a Grader are each named by a directory entry, so one shape
// governs all three.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type caseManifest struct {
	Name              string   `json:"name"`
	RepositoryURL     string   `json:"repository_url"`
	ParentCommit      string   `json:"parent_commit"`
	ReferenceRange    string   `json:"reference_range"`
	GateCommands      []string `json:"gate_commands"`
	Scope             []string `json:"scope"`
	StandardDocuments []string `json:"standard_documents"`
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ", ") }
func (s *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("value must not be empty")
	}
	*s = append(*s, value)
	return nil
}

type prepareOptions struct {
	repositoryPath string
	setID          string
	name           string
	outputRoot     string
	gates          []string
	scope          []string
	standards      []string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected a command (available: prepare, draft-acceptance, run, grade, rollup, clean)")
	}
	switch args[0] {
	case "clean":
		return runCleanCommand(args[1:])
	case "grade":
		return runGradeCommand(args[1:])
	case "run":
		return runTrialCommand(args[1:])
	case "rollup":
		return runRollupCommand(args[1:])
	case "prepare":
		return runPrepareCommand(args[1:])
	case "draft-acceptance":
		return runDraftAcceptanceCommand(args[1:])
	default:
		return fmt.Errorf("unknown command %q (available: prepare, draft-acceptance, run, grade, rollup, clean)", args[0])
	}
}

func runDraftAcceptanceCommand(args []string) error {
	flags := flag.NewFlagSet("draft-acceptance", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	agent := flags.String("agent", tasks.DefaultAgentPreset, "Drafting Agent preset and optional arguments")
	casesRoot := flags.String("cases", defaultCasesRoot, "Case directory root")
	workRoot := flags.String("work", defaultWorkRoot, "Temporary Eval work directory")
	timeout := flags.Duration("timeout", time.Hour, "Drafting Agent ceiling")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: go run ./eval draft-acceptance [flags] <case>")
	}
	path, err := draftAcceptance(draftAcceptanceOptions{casePath: flags.Arg(0), casesRoot: *casesRoot, workRoot: *workRoot, agentSpec: *agent, timeout: *timeout})
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

func runPrepareCommand(args []string) error {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var gates, scope, standards stringList
	name := flags.String("name", "", "Case name (defaults to the Task-set identifier)")
	output := flags.String("output", defaultCasesRoot, "Case directory root")
	flags.Var(&gates, "gate", "Objective gate command; repeat for more than one")
	flags.Var(&scope, "scope", "Allowed repository path; repeat for more than one")
	flags.Var(&standards, "standard", "Repository standard document; repeat for more than one")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: go run ./eval prepare [flags] <repo-path> <set-id>")
	}
	opts := prepareOptions{
		repositoryPath: flags.Arg(0),
		setID:          flags.Arg(1),
		name:           *name,
		outputRoot:     *output,
		gates:          gates,
		scope:          scope,
		standards:      standards,
	}
	path, err := prepareCase(opts)
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

func prepareCase(opts prepareOptions) (string, error) {
	repoPath, err := filepath.Abs(opts.repositoryPath)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	if opts.name == "" {
		opts.name = opts.setID
	}
	if !namePattern.MatchString(opts.name) {
		return "", fmt.Errorf("invalid Case name %q", opts.name)
	}

	d := tasks.DefaultDeps()
	identity, err := tasks.ResolveRepositoryIdentity(d, repoPath)
	if err != nil {
		return "", fmt.Errorf("resolve Task storage: %w", err)
	}
	setDir := filepath.Join(identity.TasksDir, opts.setID)
	setManifest := tasks.LoadManifest(d, opts.setID, filepath.Join(setDir, tasks.ManifestFileName))
	if !setManifest.Valid {
		if len(setManifest.Errors) == 0 {
			return "", fmt.Errorf("Task set %q is not valid", opts.setID)
		}
		return "", fmt.Errorf("Task set %q is not valid: %s", opts.setID, strings.Join(setManifest.Errors, "; "))
	}

	repositoryURL, err := git(repoPath, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("read origin remote: %w", err)
	}
	if !portableRepositoryURL(repositoryURL) {
		return "", fmt.Errorf("origin remote %q is not a portable clone URL", repositoryURL)
	}
	parent, lastReference, err := referenceCommits(repoPath, opts.setID)
	if err != nil {
		return "", err
	}

	if len(opts.gates) == 0 {
		opts.gates = []string{"go build ./...", "go test ./..."}
	}
	if len(opts.scope) == 0 {
		opts.scope = []string{"."}
	}
	if len(opts.standards) == 0 {
		opts.standards, err = defaultStandardDocuments(repoPath, parent)
		if err != nil {
			return "", err
		}
	}
	if err := validateRepositoryPaths("scope", opts.scope); err != nil {
		return "", err
	}
	if err := validateRepositoryPaths("standard document", opts.standards); err != nil {
		return "", err
	}

	manifest := caseManifest{
		Name:              opts.name,
		RepositoryURL:     repositoryURL,
		ParentCommit:      parent,
		ReferenceRange:    parent + ".." + lastReference,
		GateCommands:      opts.gates,
		Scope:             opts.scope,
		StandardDocuments: opts.standards,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode Case manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')

	prepared, err := prepareTaskSet(setManifest, setDir)
	if err != nil {
		return "", err
	}
	prepared.manifest = manifestData

	if err := os.MkdirAll(opts.outputRoot, 0o755); err != nil {
		return "", fmt.Errorf("create Case root: %w", err)
	}
	destination := filepath.Join(opts.outputRoot, opts.name)
	if _, err := os.Stat(destination); err == nil {
		return "", fmt.Errorf("Case %q already exists at %s", opts.name, destination)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect Case destination: %w", err)
	}
	staging, err := os.MkdirTemp(opts.outputRoot, ".prepare-*")
	if err != nil {
		return "", fmt.Errorf("create Case staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := prepared.write(staging); err != nil {
		return "", err
	}
	if err := os.Rename(staging, destination); err != nil {
		return "", fmt.Errorf("publish Case: %w", err)
	}
	return destination, nil
}

func validateRepositoryPaths(kind string, paths []string) error {
	for _, path := range paths {
		clean := filepath.Clean(path)
		if filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s path %q must be relative to the repository", kind, path)
		}
	}
	return nil
}

func portableRepositoryURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "file://") || filepath.IsAbs(value) {
		return false
	}
	return strings.Contains(value, "://") || strings.Contains(value, "@")
}

func git(repoPath string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func referenceCommits(repoPath, setID string) (string, string, error) {
	format := "%H%x1f%(trailers:key=" + tasks.TaskTrailerKey + ",valueonly,separator=%x1e)%x1f%(trailers:key=" + tasks.RefineTrailerKey + ",valueonly,separator=%x1e)%x00"
	output, err := git(repoPath, "log", "--reverse", "--topo-order", "--format="+format, "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("read repository history: %w", err)
	}
	var firstTask, lastReference string
	for _, record := range strings.Split(output, "\x00") {
		fields := strings.Split(strings.TrimSpace(record), "\x1f")
		if len(fields) != 3 {
			continue
		}
		sha := strings.TrimSpace(fields[0])
		taskMatch := trailerContains(fields[1], setID+"/", true)
		refineMatch := trailerContains(fields[2], setID, false)
		if taskMatch && firstTask == "" {
			firstTask = sha
		}
		if taskMatch || refineMatch {
			lastReference = sha
		}
	}
	if firstTask == "" {
		return "", "", fmt.Errorf("no %s commit found for Task set %q", tasks.TaskTrailerKey, setID)
	}
	parent, err := git(repoPath, "rev-parse", firstTask+"^")
	if err != nil {
		return "", "", fmt.Errorf("the earliest %s commit for %q has no parent: %w", tasks.TaskTrailerKey, setID, err)
	}
	return parent, lastReference, nil
}

func trailerContains(values, wanted string, prefix bool) bool {
	for _, value := range strings.Split(values, "\x1e") {
		value = strings.TrimSpace(value)
		if (!prefix && value == wanted) || (prefix && strings.HasPrefix(value, wanted)) {
			return true
		}
	}
	return false
}

func defaultStandardDocuments(repoPath, parent string) ([]string, error) {
	var found []string
	for _, path := range []string{"docs/agents/implementation.md", "AGENTS.md"} {
		if _, err := git(repoPath, "cat-file", "-e", parent+":"+path); err == nil {
			found = append(found, path)
		}
	}
	return found, nil
}

// preparedCase is the file set a Case directory is written from: the Case
// manifest, the stripped Task-set manifest, the AFK task bodies keyed by their
// file name, and the two documents lifted out of those bodies.
type preparedCase struct {
	manifest     []byte
	taskManifest []byte
	taskFiles    map[string][]byte
	spec         []byte
	acceptance   []byte
}

func prepareTaskSet(manifest *tasks.Manifest, setDir string) (preparedCase, error) {
	stripped := make(map[string]bool)
	for _, task := range manifest.Tasks {
		if task.Type == "HITL" {
			stripped[task.ID] = true
		}
	}

	cleanTasks := make([]tasks.Task, 0, len(manifest.Tasks)-len(stripped))
	taskFiles := make(map[string][]byte, len(manifest.Tasks)-len(stripped))
	var specParts, criteria []string
	for _, task := range manifest.Tasks {
		if stripped[task.ID] {
			continue
		}
		body, err := os.ReadFile(filepath.Join(setDir, task.File))
		if err != nil {
			return preparedCase{}, fmt.Errorf("read task %q: %w", task.ID, err)
		}
		taskFiles[task.File] = body
		specParts = append(specParts, strings.TrimRight(string(body), "\n"))
		criteria = append(criteria, acceptanceCheckboxes(body)...)

		task.Status = tasks.TaskOpen
		task.FailedAfter = nil
		task.Commit = nil
		task.Origin = ""
		blockers := task.BlockedBy[:0]
		for _, blocker := range task.BlockedBy {
			if !stripped[blocker] {
				blockers = append(blockers, blocker)
			}
		}
		task.BlockedBy = blockers
		cleanTasks = append(cleanTasks, task)
	}
	if len(cleanTasks) == 0 {
		return preparedCase{}, errors.New("Task set has no AFK tasks")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(manifest.Raw, &raw); err != nil {
		return preparedCase{}, fmt.Errorf("decode Task-set manifest: %w", err)
	}
	tasksData, err := json.Marshal(cleanTasks)
	if err != nil {
		return preparedCase{}, fmt.Errorf("encode stripped tasks: %w", err)
	}
	raw["tasks"] = tasksData
	for _, key := range []string{"base_commit", "human_completed", "worktree", "auto_drain", "source_map"} {
		delete(raw, key)
	}
	cleanManifest, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return preparedCase{}, fmt.Errorf("encode stripped Task-set manifest: %w", err)
	}
	cleanManifest = append(cleanManifest, '\n')

	return preparedCase{
		taskManifest: cleanManifest,
		taskFiles:    taskFiles,
		spec:         []byte(strings.Join(specParts, "\n\n") + "\n"),
		acceptance:   []byte("# Acceptance list\n\nStatus: not approved\n\n## Lifted criteria\n\n" + strings.Join(criteria, "\n") + "\n"),
	}, nil
}

func acceptanceCheckboxes(body []byte) []string {
	lines := strings.Split(string(body), "\n")
	inAcceptance := false
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "## "+tasks.AcceptanceCriteriaHeading) {
			inAcceptance = true
			continue
		}
		if inAcceptance && strings.HasPrefix(trimmed, "## ") {
			break
		}
		if inAcceptance && len(trimmed) >= 6 && strings.HasPrefix(trimmed, "- [") && trimmed[4] == ']' {
			result = append(result, "- [ ]"+trimmed[5:])
		}
	}
	return result
}

// write lays the Case out in the directory shape eval/README.md documents.
func (c preparedCase) write(dir string) error {
	taskDir := filepath.Join(dir, "tasks")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return fmt.Errorf("create Case tasks directory: %w", err)
	}
	files := map[string][]byte{
		filepath.Join(dir, caseManifestName):           c.manifest,
		filepath.Join(dir, tasks.SpecFileName):         c.spec,
		filepath.Join(dir, acceptanceName):             c.acceptance,
		filepath.Join(taskDir, tasks.ManifestFileName): c.taskManifest,
	}
	for name, data := range c.taskFiles {
		files[filepath.Join(taskDir, name)] = data
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}
