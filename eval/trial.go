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
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var caseNames, armNames stringList
	var repeats intList
	flags.Var(&caseNames, "case", "Case name; repeat to select more than one")
	flags.Var(&armNames, "arm", "Arm file name; repeat to select more than one")
	flags.Var(&repeats, "repeat", "Trial repeat number; repeat to select more than one")
	cases := flags.String("cases", filepath.Join("eval", "cases"), "Case directory root")
	arms := flags.String("arms", filepath.Join("eval", "arms"), "Arm directory root")
	graders := flags.String("graders", filepath.Join("eval", "graders"), "Grader arm directory root")
	configPath := flags.String("config", filepath.Join("eval", "config.toml"), "Harness config")
	work := flags.String("work", filepath.Join("eval", "work"), "Eval work directory")
	results := flags.String("results", filepath.Join("eval", "results"), "Trial record directory root")
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
	trialOpts := trialOptions{cases: *cases, arms: *arms, work: *work, results: *results, pop: *pop, ceiling: *ceiling}
	return runMatrix(selectedCases, armNames, repeats, *results,
		func(name, arm string, repeat int) (string, error) {
			return runOneTrial(name, arm, repeat, trialOpts)
		},
		func(name, arm string, repeat int) error {
			gradeArgs := []string{"--cases", *cases, "--arms", *arms, "--graders", *graders, "--config", *configPath, "--work", *work, "--results", *results, "--timeout", gradeTimeout.String(), name, arm, strconv.Itoa(repeat)}
			return runGradeCommand(gradeArgs)
		})
}

func runMatrix(cases, arms []string, repeats []int, results string, runTrial func(string, string, int) (string, error), gradeTrial func(string, string, int) error) error {
	for _, repeat := range repeats {
		for _, name := range cases {
			for _, arm := range arms {
				resultDir := filepath.Join(results, name, arm, fmt.Sprintf("%02d", repeat))
				recordPath := filepath.Join(resultDir, "trial.json")
				existing, exists, err := readTrialRecord(recordPath)
				if err != nil {
					return err
				}
				outcome := ""
				if !exists || (existing.Outcome == "invalid" && existing.Attempts < 2) {
					outcome, err = runTrial(name, arm, repeat)
					if outcome == "invalid" {
						outcome, err = runTrial(name, arm, repeat)
					}
					if err != nil && outcome != "invalid" {
						return err
					}
				} else if existing.Grade != nil {
					fmt.Printf("skip %s\n", resultDir)
					continue
				}
				if err := gradeTrial(name, arm, repeat); err != nil {
					return err
				}
			}
		}
	}
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
		if !entry.IsDir() || !caseNamePattern.MatchString(entry.Name()) {
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
	if !caseNamePattern.MatchString(manifest.Name) || manifest.Name != name {
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
	record := trialRecord{Case: manifest.Name, Arm: armName, Repeat: repeat, Attempts: attempts, Model: selected.Model, WorkDir: workDir, StartedAt: time.Now().UTC(), Outcome: "invalid"}
	cloneDir := filepath.Join(workDir, "repository")
	trialErr := cloneAtCommit(manifest.RepositoryURL, manifest.ParentCommit, cloneDir)
	var patch []byte
	if trialErr == nil {
		if selected.Kind == "pop" {
			trialErr = runPopTrial(opts.pop, caseDir, cloneDir, selected, opts.ceiling, &record)
		} else {
			attempt, captureErr := tasks.RunCapturedAgentInvocation(tasks.DefaultDeps(), tasks.CapturedAgentOptions{
				AgentSpec: selected.agentSpec(), Prompt: barePrompt(manifest, string(spec)), RuntimePath: cloneDir,
				Timeout: opts.ceiling, DestinationDir: filepath.Join(workDir, "capture"),
			})
			trialErr = captureErr
			if attempt != nil {
				record.ActualModel, record.RunID = attempt.ActualModel, attempt.RunID
				record.Spend, record.Notional = attempt.Spend, attempt.Notional
				record.AgentOutcome, record.Reason = attempt.Outcome, attempt.Reason
				if captureErr == nil && (attempt.Outcome == "completed" || attempt.Outcome == "timed_out") {
					record.Outcome = attempt.Outcome
				}
			}
		}
		var patchErr error
		patch, patchErr = trialPatch(cloneDir, manifest.ParentCommit)
		trialErr = errors.Join(trialErr, patchErr)
	}
	record.EndedAt = time.Now().UTC()
	if trialErr != nil {
		record.Outcome, record.Reason = "invalid", trialErr.Error()
	}
	if err := tasks.WriteAtomic(filepath.Join(resultDir, "diff.patch"), patch, 0o644); err != nil {
		return "", fmt.Errorf("write Trial patch: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	if err := tasks.WriteAtomic(filepath.Join(resultDir, "trial.json"), append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write Trial record: %w", err)
	}
	fmt.Println(resultDir)
	return record.Outcome, trialErr
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
