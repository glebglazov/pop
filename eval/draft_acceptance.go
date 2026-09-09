package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

const (
	draftingDirName    = "drafting"
	draftingRecordName = "result.json"
)

var numberedBehaviour = regexp.MustCompile(`^\s*\d+[.)]\s+(.+?)\s*$`)

type draftAcceptanceOptions struct {
	casePath  string
	casesRoot string
	workRoot  string
	agentSpec string
	timeout   time.Duration
}

// capturedRunRecord is what one captured Agent invocation left behind: which
// agent ran, the run it filed, and what it spent. The Acceptance-list drafting
// run and the Grader each store one, both outside any Arm's spend.
type capturedRunRecord struct {
	Agent       string            `json:"agent"`
	RunID       string            `json:"run_id"`
	Outcome     string            `json:"outcome"`
	ActualModel string            `json:"actual_model,omitempty"`
	Spend       tasks.RunSpend    `json:"spend"`
	Notional    tasks.PricedSpend `json:"notional"`
}

func draftAcceptance(opts draftAcceptanceOptions) (string, error) {
	caseDir, err := resolveCaseDir(opts.casePath, opts.casesRoot)
	if err != nil {
		return "", err
	}
	manifest, err := readCaseManifest(caseDir)
	if err != nil {
		return "", err
	}
	spec, err := os.ReadFile(filepath.Join(caseDir, tasks.SpecFileName))
	if err != nil {
		return "", fmt.Errorf("read Case spec: %w", err)
	}
	acceptancePath := filepath.Join(caseDir, acceptanceName)
	skeleton, err := os.ReadFile(acceptancePath)
	if err != nil {
		return "", fmt.Errorf("read Acceptance list skeleton: %w", err)
	}
	lifted, err := liftedCriteria(string(skeleton))
	if err != nil {
		return "", fmt.Errorf("read lifted criteria from %s: %w", acceptancePath, err)
	}

	if err := os.MkdirAll(opts.workRoot, 0o755); err != nil {
		return "", fmt.Errorf("create Eval work directory: %w", err)
	}
	workDir, err := os.MkdirTemp(opts.workRoot, manifest.Name+"-acceptance-*")
	if err != nil {
		return "", fmt.Errorf("create Acceptance-list work directory: %w", err)
	}
	defer os.RemoveAll(workDir)
	cloneDir := filepath.Join(workDir, "repository")
	if err := cloneAtCommit(manifest.RepositoryURL, manifest.ParentCommit, cloneDir); err != nil {
		return "", err
	}
	referenceDiff, err := git(cloneDir, "diff", manifest.ReferenceRange)
	if err != nil {
		return "", fmt.Errorf("read Reference diff: %w", err)
	}

	prompt := draftingPrompt(string(spec), lifted, referenceDiff)
	runsDir := filepath.Join(caseDir, draftingDirName, "runs")
	attempt, err := tasks.RunCapturedAgentInvocation(tasks.DefaultDeps(), tasks.CapturedAgentOptions{
		AgentSpec: opts.agentSpec, Prompt: prompt, RuntimePath: cloneDir,
		Timeout: opts.timeout, DestinationDir: runsDir,
	})
	if err != nil {
		return "", fmt.Errorf("run Acceptance-list drafting agent: %w", err)
	}
	if err := writeDraftingRecord(filepath.Join(caseDir, draftingDirName, draftingRecordName), opts.agentSpec, attempt); err != nil {
		return "", err
	}
	if attempt.Outcome != "completed" {
		return "", fmt.Errorf("Acceptance-list drafting agent ended with outcome %s", attempt.Outcome)
	}
	behaviours, err := parseDraftedBehaviours(attempt.Output)
	if err != nil {
		return "", err
	}
	var result strings.Builder
	result.WriteString("# Acceptance list\n\nStatus: draft\n\n")
	for i, behaviour := range behaviours {
		fmt.Fprintf(&result, "%d. %s\n", i+1, behaviour)
	}
	if err := tasks.WriteAtomic(acceptancePath, []byte(result.String()), 0o644); err != nil {
		return "", fmt.Errorf("write drafted Acceptance list: %w", err)
	}
	return acceptancePath, nil
}

func resolveCaseDir(casePath, casesRoot string) (string, error) {
	if info, err := os.Stat(casePath); err == nil && info.IsDir() {
		return filepath.Abs(casePath)
	}
	if !namePattern.MatchString(casePath) {
		return "", fmt.Errorf("invalid Case name %q", casePath)
	}
	path := filepath.Join(casesRoot, casePath)
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		return "", fmt.Errorf("find Case %q at %s: %w", casePath, path, err)
	}
	return filepath.Abs(path)
}

func readCaseManifest(caseDir string) (caseManifest, error) {
	path := filepath.Join(caseDir, caseManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		return caseManifest{}, fmt.Errorf("read Case manifest: %w", err)
	}
	var manifest caseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return caseManifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if manifest.Name == "" || manifest.RepositoryURL == "" || manifest.ParentCommit == "" || manifest.ReferenceRange == "" {
		return caseManifest{}, fmt.Errorf("%s does not name the Case, repository, parent commit, and Reference range", path)
	}
	return manifest, nil
}

func liftedCriteria(skeleton string) (string, error) {
	const heading = "## Lifted criteria"
	_, body, found := strings.Cut(skeleton, heading)
	if !found || strings.TrimSpace(body) == "" {
		return "", errors.New("missing Lifted criteria section")
	}
	return strings.TrimSpace(body), nil
}

func cloneAtCommit(repositoryURL, commit, destination string) error {
	command := exec.Command("git", "clone", repositoryURL, destination)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("clone Case repository: %s", strings.TrimSpace(string(output)))
	}
	for _, setting := range []struct{ key, value string }{
		{"user.name", "Pop Eval Harness"},
		{"user.email", "eval@pop.invalid"},
		{"commit.gpgsign", "false"},
	} {
		if _, err := git(destination, "config", "--local", setting.key, setting.value); err != nil {
			return fmt.Errorf("configure Case clone %s: %w", setting.key, err)
		}
	}
	if _, err := git(destination, "checkout", "--detach", commit); err != nil {
		return fmt.Errorf("check out Case parent commit: %w", err)
	}
	return nil
}

func draftingPrompt(spec, lifted, referenceDiff string) string {
	return `Draft the Acceptance list for this Eval Case.

Record observable behaviours only. Never record code shape. Each behaviour must be checkable against a diff. Do not copy implementation details from the Reference diff.

Return only a numbered list. Put one complete behaviour on each line in this exact shape:
1. <observable behaviour>

## Spec

` + strings.TrimSpace(spec) + `

## Lifted criteria

` + strings.TrimSpace(lifted) + `

## Reference diff

` + strings.TrimSpace(referenceDiff) + "\n"
}

func parseDraftedBehaviours(output string) ([]string, error) {
	var behaviours []string
	for _, line := range strings.Split(output, "\n") {
		match := numberedBehaviour.FindStringSubmatch(line)
		if len(match) == 2 {
			behaviours = append(behaviours, match[1])
			continue
		}
		if strings.TrimSpace(line) != "" {
			return nil, fmt.Errorf("drafting agent returned a non-numbered line %s", strconv.Quote(strings.TrimSpace(line)))
		}
	}
	if len(behaviours) == 0 {
		return nil, errors.New("drafting agent returned no numbered behaviours")
	}
	return behaviours, nil
}

func writeDraftingRecord(path, agent string, attempt *tasks.CapturedAgentAttempt) error {
	record := capturedRunRecord{Agent: agent, RunID: attempt.RunID, Outcome: attempt.Outcome, ActualModel: attempt.ActualModel, Spend: attempt.Spend, Notional: attempt.Notional}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode drafting spend: %w", err)
	}
	if err := tasks.WriteAtomic(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write drafting spend: %w", err)
	}
	return nil
}

func loadApprovedAcceptance(caseDir string) (string, error) {
	path := filepath.Join(caseDir, acceptanceName)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read Acceptance list: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "Status: approved") {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("Case is not approved; change the status line in %s to Status: approved", path)
}
