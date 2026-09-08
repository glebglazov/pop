package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

type trialRecord struct {
	Grade        *gradeRecord      `json:"grade,omitempty"`
	Case         string            `json:"case"`
	Arm          string            `json:"arm"`
	Repeat       int               `json:"repeat"`
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
	name := flags.String("case", "", "Case name")
	arm := flags.String("arm", "bare", "Arm file name")
	cases := flags.String("cases", filepath.Join("eval", "cases"), "Case directory root")
	arms := flags.String("arms", filepath.Join("eval", "arms"), "Arm directory root")
	work := flags.String("work", filepath.Join("eval", "work"), "Eval work directory")
	results := flags.String("results", filepath.Join("eval", "results"), "Trial record directory root")
	repeat := flags.Int("repeat", 1, "Trial repeat number")
	pop := flags.String("pop", "pop", "Pop binary path")
	ceiling := flags.Duration("ceiling", 4*time.Hour, "Trial ceiling")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *name == "" || *repeat < 1 || *ceiling <= 0 {
		return errors.New("usage: go run ./eval run --case <name> --arm <arm> [--repeat N] [--ceiling 4h]")
	}
	caseDir, err := resolveCaseDir(*name, *cases)
	if err != nil {
		return err
	}
	manifest, err := readCaseManifest(caseDir)
	if err != nil {
		return err
	}
	if !caseNamePattern.MatchString(manifest.Name) {
		return fmt.Errorf("invalid Case name %q", manifest.Name)
	}
	if _, err := loadApprovedAcceptance(caseDir); err != nil {
		return err
	}
	selected, err := loadArm(*arms, *arm)
	if err != nil {
		return err
	}
	spec, err := os.ReadFile(filepath.Join(caseDir, tasks.SpecFileName))
	if err != nil {
		return fmt.Errorf("read Case spec: %w", err)
	}
	resultDir := filepath.Join(*results, manifest.Name, *arm, fmt.Sprintf("%02d", *repeat))
	if err := os.MkdirAll(filepath.Dir(resultDir), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(resultDir, 0o755); err != nil {
		return fmt.Errorf("create Trial result directory (repeat must be unused): %w", err)
	}
	if err := os.MkdirAll(*work, 0o755); err != nil {
		return err
	}
	workRoot, err := filepath.Abs(*work)
	if err != nil {
		return err
	}
	workDir, err := os.MkdirTemp(workRoot, "trial-*")
	if err != nil {
		return err
	}
	record := trialRecord{Case: manifest.Name, Arm: *arm, Repeat: *repeat, Model: selected.Model, WorkDir: workDir, StartedAt: time.Now().UTC(), Outcome: "invalid"}
	cloneDir := filepath.Join(workDir, "repository")
	trialErr := cloneAtCommit(manifest.RepositoryURL, manifest.ParentCommit, cloneDir)
	var patch []byte
	if trialErr == nil {
		if selected.Kind == "pop" {
			trialErr = runPopTrial(*pop, caseDir, cloneDir, selected, *ceiling, &record)
		} else {
			attempt, captureErr := tasks.RunCapturedAgentInvocation(tasks.DefaultDeps(), tasks.CapturedAgentOptions{
				AgentSpec: selected.agentSpec(), Prompt: barePrompt(manifest, string(spec)), RuntimePath: cloneDir,
				Timeout: *ceiling, DestinationDir: filepath.Join(workDir, "capture"),
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
		return fmt.Errorf("write Trial patch: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := tasks.WriteAtomic(filepath.Join(resultDir, "trial.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write Trial record: %w", err)
	}
	fmt.Println(resultDir)
	return trialErr
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
