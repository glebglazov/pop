package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// The three outcomes a Trial can end in. A Rollup counts on them, a Grader
// gates on them, and both arms have to reach one of them.
const (
	outcomeCompleted = "completed"
	outcomeTimedOut  = "timed_out"
	outcomeInvalid   = "invalid"
)

type trialRecord struct {
	Grade        *gradeRecord      `json:"grade,omitempty"`
	Case         string            `json:"case"`
	Arm          string            `json:"arm"`
	Repeat       int               `json:"repeat"`
	Attempts     int               `json:"attempts"`
	Model        string            `json:"model"`
	ActualModel  string            `json:"actual_model,omitempty"`
	StartedAt    time.Time         `json:"started_at"`
	EndedAt      time.Time         `json:"ended_at"`
	Outcome      string            `json:"outcome"`
	Reason       string            `json:"reason,omitempty"`
	AgentOutcome string            `json:"agent_outcome,omitempty"`
	RunID        string            `json:"run_id,omitempty"`
	WorkDir      string            `json:"work_dir"`
	SetStatus    string            `json:"set_status,omitempty"`
	SetSpend     json.RawMessage   `json:"set_spend,omitempty"`
	Spend        tasks.RunSpend    `json:"spend"`
	Notional     tasks.PricedSpend `json:"notional"`
}

func runTrialCommand(args []string) error {
	return runTrialCommandWithProgress(args, evalProgress{out: os.Stderr})
}

func runTrialCommandWithProgress(args []string, progress evalProgress) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var caseNames, armNames stringList
	var repeats intList
	flags.Var(&caseNames, "case", "Case name; repeat to select more than one")
	flags.Var(&armNames, "arm", "Arm file name; repeat to select more than one")
	flags.Var(&repeats, "repeat", "Trial repeat number; repeat to select more than one")
	cases := flags.String("cases", defaultCasesRoot, "Case directory root")
	arms := flags.String("arms", defaultArmsRoot, "Arm directory root")
	graders := flags.String("graders", defaultGradersRoot, "Grader arm directory root")
	configPath := flags.String("config", defaultConfigPath, "Harness config")
	work := flags.String("work", defaultWorkRoot, "Eval work directory")
	results := flags.String("results", defaultResultsRoot, "Trial record directory root")
	pop := flags.String("pop", "pop", "Pop binary path")
	ceiling := flags.Duration("ceiling", 4*time.Hour, "Trial ceiling")
	gradeTimeout := flags.Duration("grade-timeout", time.Hour, "Grader ceiling")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *ceiling <= 0 || *gradeTimeout <= 0 {
		return errors.New("usage: go run ./eval run [--case <name>] [--arm <arm>] [--repeat N] [--ceiling 4h]")
	}
	selectedCases, err := selectCases(*cases, caseNames)
	if err != nil {
		return err
	}
	if len(armNames) == 0 {
		armNames = stringList{"bare", "pop"}
	}
	if len(repeats) == 0 {
		repeats = intList{1}
	}
	if err := uniqueSelections("Case", []string(selectedCases)); err != nil {
		return err
	}
	if err := uniqueSelections("Arm", []string(armNames)); err != nil {
		return err
	}
	if err := repeats.validate(); err != nil {
		return err
	}
	for _, arm := range armNames {
		if _, err := loadArm(*arms, arm); err != nil {
			return err
		}
	}
	sort.Ints(repeats)
	trialOpts := trialOptions{cases: *cases, arms: *arms, work: *work, results: *results, pop: *pop, ceiling: *ceiling, progress: progress}
	return runMatrix(selectedCases, armNames, repeats, *results, progress,
		func(name, arm string, repeat int) (string, error) {
			return runOneTrial(name, arm, repeat, trialOpts)
		},
		func(name, arm string, repeat int) error {
			gradeArgs := []string{"--cases", *cases, "--arms", *arms, "--graders", *graders, "--config", *configPath, "--work", *work, "--results", *results, "--timeout", gradeTimeout.String(), name, arm, strconv.Itoa(repeat)}
			return runGradeCommandWithProgress(gradeArgs, progress, false)
		})
}

func runMatrix(cases, arms []string, repeats []int, results string, progress evalProgress, runTrial func(string, string, int) (string, error), gradeOneTrial func(string, string, int) error) error {
	resolvedResults, err := filepath.Abs(results)
	if err != nil {
		return fmt.Errorf("resolve results directory: %w", err)
	}
	total := len(cases) * len(arms) * len(repeats)
	progress.line("Eval Matrix: Trials=%d results=%s", total, resolvedResults)
	position := 0
	for _, repeat := range repeats {
		for _, name := range cases {
			for _, arm := range arms {
				position++
				label := trialLabel(name, arm, repeat)
				resultDir := filepath.Join(results, name, arm, fmt.Sprintf("%02d", repeat))
				recordPath := filepath.Join(resultDir, "trial.json")
				existing, exists, err := readTrialRecord(recordPath)
				if err != nil {
					return fmt.Errorf("Trial %s phase resume: %w", label, err)
				}
				outcome := ""
				if !exists || (existing.Outcome == outcomeInvalid && existing.Attempts < 2) {
					attempt := 1
					if exists {
						attempt = existing.Attempts + 1
						progress.line("Trial %d/%d %s: Invalid Trial retry attempt=%d reason=%s", position, total, label, attempt, existing.Reason)
					} else {
						progress.line("Trial %d/%d %s attempt=%d", position, total, label, attempt)
					}
					outcome, err = runTrial(name, arm, repeat)
					if outcome == outcomeInvalid {
						record, _, readErr := readTrialRecord(recordPath)
						if readErr != nil {
							return fmt.Errorf("Trial %s phase Invalid Trial retry: %w", label, readErr)
						}
						progress.line("Trial %d/%d %s: Invalid Trial retry attempt=%d reason=%s", position, total, label, record.Attempts+1, record.Reason)
						outcome, err = runTrial(name, arm, repeat)
					}
					if err != nil && outcome != outcomeInvalid {
						return fmt.Errorf("Trial %s: %w", label, err)
					}
				} else if existing.Grade != nil {
					progress.line("Trial %d/%d %s attempt=%d skipped: saved Trial already has grade=%s result=%s", position, total, label, existing.Attempts, existing.Grade.Status, resultDir)
					continue
				} else {
					progress.line("Trial %d/%d %s attempt=%d resumed: saved patch will be graded without another Arm invocation", position, total, label, existing.Attempts)
				}
				if err := gradeOneTrial(name, arm, repeat); err != nil {
					return fmt.Errorf("Trial %s phase grading: %w", label, err)
				}
				record, _, err := readTrialRecord(recordPath)
				if err != nil {
					return fmt.Errorf("Trial %s phase completion: %w", label, err)
				}
				progress.line("Trial %d/%d %s finished: outcome=%s %s result=%s", position, total, label, record.Outcome, gradeSummary(record), resultDir)
			}
		}
	}
	counts := map[string]int{}
	grades := map[string]int{}
	for _, repeat := range repeats {
		for _, name := range cases {
			for _, arm := range arms {
				record, exists, err := readTrialRecord(filepath.Join(results, name, arm, fmt.Sprintf("%02d", repeat), "trial.json"))
				if err != nil {
					return fmt.Errorf("read Matrix result: %w", err)
				}
				if exists {
					counts[record.Outcome]++
					if record.Grade != nil {
						grades[record.Grade.Status]++
					}
				}
			}
		}
	}
	progress.line("Eval Matrix finished: completed=%d timed_out=%d invalid=%d graded=%d gate_failed=%d ungraded=%d", counts[outcomeCompleted], counts[outcomeTimedOut], counts[outcomeInvalid], grades["graded"], grades["gate_failed"], grades["ungraded"])
	progress.line("Read the Rollup: go run ./eval rollup --results %s", resolvedResults)
	return nil
}

func readTrialRecord(path string) (trialRecord, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return trialRecord{}, false, nil
	}
	if err != nil {
		return trialRecord{}, false, err
	}
	var record trialRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return trialRecord{}, false, fmt.Errorf("read Trial record %s: %w", path, err)
	}
	return record, true, nil
}

type intList []int

func (v *intList) String() string {
	parts := make([]string, len(*v))
	for i, n := range *v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

func (v *intList) Set(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return errors.New("repeat must be a positive integer")
	}
	*v = append(*v, n)
	return nil
}

func (v intList) validate() error {
	seen := map[int]bool{}
	for _, n := range v {
		if seen[n] {
			return fmt.Errorf("repeat %d selected more than once", n)
		}
		seen[n] = true
	}
	return nil
}

func uniqueSelections(kind string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return fmt.Errorf("%s %q selected more than once", kind, value)
		}
		seen[value] = true
	}
	return nil
}

func selectCases(root string, selected []string) ([]string, error) {
	if len(selected) > 0 {
		for _, name := range selected {
			caseDir, err := resolveCaseDir(name, root)
			if err != nil {
				return nil, err
			}
			if _, err := loadApprovedAcceptance(caseDir); err != nil {
				return nil, err
			}
		}
		return selected, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var approved []string
	for _, entry := range entries {
		if !entry.IsDir() || !namePattern.MatchString(entry.Name()) {
			continue
		}
		caseDir := filepath.Join(root, entry.Name())
		manifest, err := readCaseManifest(caseDir)
		if err != nil || manifest.Name != entry.Name() {
			continue
		}
		if _, err := loadApprovedAcceptance(caseDir); err == nil {
			approved = append(approved, entry.Name())
		}
	}
	if len(approved) == 0 {
		return nil, errors.New("no approved Cases found")
	}
	return approved, nil
}

type trialOptions struct {
	cases, arms, work, results, pop string
	ceiling                         time.Duration
	progress                        evalProgress
}

func runOneTrial(name, armName string, repeat int, opts trialOptions) (string, error) {
	caseDir, err := resolveCaseDir(name, opts.cases)
	if err != nil {
		return "", err
	}
	manifest, err := readCaseManifest(caseDir)
	if err != nil {
		return "", err
	}
	if !namePattern.MatchString(manifest.Name) || manifest.Name != name {
		return "", fmt.Errorf("invalid Case name %q", manifest.Name)
	}
	if _, err := loadApprovedAcceptance(caseDir); err != nil {
		return "", err
	}
	selected, err := loadArm(opts.arms, armName)
	if err != nil {
		return "", err
	}
	spec, err := os.ReadFile(filepath.Join(caseDir, tasks.SpecFileName))
	if err != nil {
		return "", fmt.Errorf("read Case spec: %w", err)
	}
	resultDir := filepath.Join(opts.results, manifest.Name, armName, fmt.Sprintf("%02d", repeat))
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(opts.work, 0o755); err != nil {
		return "", err
	}
	workRoot, err := filepath.Abs(opts.work)
	if err != nil {
		return "", err
	}
	workDir, err := os.MkdirTemp(workRoot, "trial-*")
	if err != nil {
		return "", err
	}
	attempts := 1
	if previous, exists, err := readTrialRecord(filepath.Join(resultDir, "trial.json")); err != nil {
		return "", err
	} else if exists {
		attempts = previous.Attempts + 1
	}
	record := trialRecord{Case: manifest.Name, Arm: armName, Repeat: repeat, Attempts: attempts, Model: selected.Model, WorkDir: workDir, StartedAt: time.Now().UTC(), Outcome: outcomeInvalid}
	label := trialLabel(manifest.Name, armName, repeat)
	cloneDir := filepath.Join(workDir, "repository")
	opts.progress.line("Trial %s attempt=%d preparation started", label, attempts)
	trialErr := cloneAtCommit(manifest.RepositoryURL, manifest.ParentCommit, cloneDir)
	var patch []byte
	if trialErr == nil {
		opts.progress.line("Trial %s attempt=%d preparation finished", label, attempts)
		opts.progress.line("Trial %s attempt=%d Arm execution started", label, attempts)
		if selected.Kind == "pop" {
			trialErr = runPopTrial(opts.pop, caseDir, cloneDir, selected, opts.ceiling, &record)
		} else {
			trialErr = runBareTrial(cloneDir, selected, manifest, string(spec), opts.ceiling, &record)
		}
		if trialErr != nil {
			opts.progress.line("Trial %s attempt=%d Arm execution finished: outcome=%s error=%v", label, attempts, record.Outcome, trialErr)
		} else {
			opts.progress.line("Trial %s attempt=%d Arm execution finished: outcome=%s", label, attempts, record.Outcome)
		}
		var patchErr error
		patch, patchErr = trialPatch(cloneDir, manifest.ParentCommit)
		trialErr = errors.Join(trialErr, patchErr)
	} else {
		opts.progress.line("Trial %s attempt=%d preparation failed: %v", label, attempts, trialErr)
	}
	record.EndedAt = time.Now().UTC()
	if trialErr != nil {
		record.Outcome, record.Reason = outcomeInvalid, trialErr.Error()
	}
	patchPath := filepath.Join(resultDir, "diff.patch")
	opts.progress.line("Trial %s attempt=%d patch saving started", label, attempts)
	if err := tasks.WriteAtomic(patchPath, patch, 0o644); err != nil {
		opts.progress.line("Trial %s attempt=%d patch saving failed: %v", label, attempts, err)
		return "", fmt.Errorf("phase patch saving: write Trial patch: %w", err)
	}
	opts.progress.line("Trial %s attempt=%d patch saving finished: path=%s", label, attempts, patchPath)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	if err := tasks.WriteAtomic(filepath.Join(resultDir, "trial.json"), append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("phase result saving: write Trial record: %w", err)
	}
	fmt.Println(resultDir)
	return record.Outcome, trialErr
}

// runBareTrial gives the Case spec to one captured invocation and reads the
// Trial outcome out of it. Only an agent that finished or hit the ceiling has
// produced a Trial worth grading; a crash or a quota pause leaves the record
// Invalid. The capture seam spells those two outcomes the way a Trial does,
// which is what lets the agent's word carry straight into the record.
func runBareTrial(clone string, arm armFile, manifest caseManifest, spec string, ceiling time.Duration, record *trialRecord) error {
	attempt, captureErr := tasks.RunCapturedAgentInvocation(tasks.DefaultDeps(), tasks.CapturedAgentOptions{
		AgentSpec: arm.agentSpec(), Prompt: barePrompt(manifest, spec), RuntimePath: clone,
		Timeout: ceiling, DestinationDir: filepath.Join(record.WorkDir, "capture"),
	})
	if attempt == nil {
		return captureErr
	}
	record.ActualModel, record.RunID = attempt.ActualModel, attempt.RunID
	record.Spend, record.Notional = attempt.Spend, attempt.Notional
	record.AgentOutcome, record.Reason = attempt.Outcome, attempt.Reason
	if captureErr == nil && (attempt.Outcome == "completed" || attempt.Outcome == "timed_out") {
		record.Outcome = attempt.Outcome
	}
	return captureErr
}

func barePrompt(manifest caseManifest, spec string) string {
	return "Implement the spec below in repository " + manifest.RepositoryURL + ".\n" +
		"Complete all requested work and run the gate commands before you finish.\n" +
		"Do not make git commits.\n\nGate commands:\n" + strings.Join(manifest.GateCommands, "\n") +
		"\n\n## Spec\n\n" + spec
}

func trialPatch(repository, parent string) ([]byte, error) {
	if _, err := git(repository, "add", "-A", "-N"); err != nil {
		return nil, err
	}
	command := exec.Command("git", "-C", repository, "diff", "--binary", "--no-ext-diff", "--no-textconv", parent, "--")
	patch, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read Trial diff: %w", err)
	}
	return patch, nil
}
