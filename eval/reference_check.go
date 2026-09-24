package main

import (
	"crypto/sha256"
	"encoding/hex"
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

const (
	referenceCheckDirName    = "reference-check"
	referenceCheckRecordName = "result.json"
)

// referenceCheck is the Grader's grade of a Case's Reference diff against its
// Acceptance list. It is kept with the Case and names what it graded, so a
// change to the list, the Grader or the Reference range makes it stale.
type referenceCheck struct {
	AcceptanceDigest string            `json:"acceptance_digest"`
	Grader           string            `json:"grader"`
	ReferenceRange   string            `json:"reference_range"`
	Grade            *gradeRecord      `json:"grade"`
	Accepted         []acceptedFailure `json:"accepted"`
}

// acceptedFailure is the human's reason to keep an item that the Reference
// fails. Behaviour holds the item's text, so a reason never passes to another
// item that a list edit moved into the same number.
type acceptedFailure struct {
	Item      int    `json:"item"`
	Behaviour string `json:"behaviour"`
	Reason    string `json:"reason"`
}

// acceptanceBehaviours returns the numbered behaviours of an Acceptance list in
// order. The Grader numbers its items by this order, not by the list's own
// numbers.
func acceptanceBehaviours(acceptance string) []string {
	var behaviours []string
	for _, line := range strings.Split(acceptance, "\n") {
		if match := numberedBehaviour.FindStringSubmatch(line); match != nil {
			behaviours = append(behaviours, match[1])
		}
	}
	return behaviours
}

// acceptanceDigest identifies the behaviours a Grader reads. A status or
// heading edit leaves it unchanged; an edit to any behaviour changes it.
func acceptanceDigest(acceptance string) string {
	sum := sha256.Sum256([]byte(strings.Join(acceptanceBehaviours(acceptance), "\n")))
	return hex.EncodeToString(sum[:])
}

func runReferenceCheckCommand(args []string) error {
	return runReferenceCheckCommandWithProgress(args, evalProgress{out: os.Stderr})
}

func runReferenceCheckCommandWithProgress(args []string, progress evalProgress) error {
	flags := flag.NewFlagSet("reference-check", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	cases := flags.String("cases", defaultCasesRoot, "Case directory root")
	arms := flags.String("arms", defaultArmsRoot, "Arm directory root")
	graders := flags.String("graders", defaultGradersRoot, "Grader arm directory root")
	configPath := flags.String("config", defaultConfigPath, "Harness config")
	work := flags.String("work", defaultWorkRoot, "Eval work directory")
	timeout := flags.Duration("timeout", time.Hour, "Grader ceiling")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || *timeout <= 0 {
		return errors.New("usage: go run ./eval reference-check [flags] <case>")
	}
	caseDir, err := resolveCaseDir(flags.Arg(0), *cases)
	if err != nil {
		return err
	}
	manifest, err := readCaseManifest(caseDir)
	if err != nil {
		return err
	}
	acceptanceData, err := os.ReadFile(filepath.Join(caseDir, acceptanceName))
	if err != nil {
		return fmt.Errorf("read Acceptance list: %w", err)
	}
	acceptance := string(acceptanceData)
	behaviours := acceptanceBehaviours(acceptance)
	if len(behaviours) == 0 {
		return errors.New("Acceptance list has no numbered behaviours")
	}
	grader, err := loadGrader(*configPath, *graders)
	if err != nil {
		return err
	}
	if err := checkGraderModel(grader, *arms); err != nil {
		return err
	}
	label := "Reference check Case=" + manifest.Name
	patch, err := referencePatch(manifest, *work)
	if err != nil {
		return fmt.Errorf("%s phase Reference diff: %w", label, err)
	}
	checkDir := filepath.Join(caseDir, referenceCheckDirName)
	grade := &gradeRecord{Status: "ungraded", AcceptanceDigest: acceptanceDigest(acceptance), BaselineGates: []gateResult{}, Gates: []gateResult{}, OutsideScope: []string{}}
	gradeErr := gradeDiff(manifest, acceptance, len(behaviours), patch, grade, label, filepath.Join(checkDir, "runs"),
		gradeOptions{work: *work, grader: grader, timeout: *timeout, progress: progress})
	if gradeErr != nil {
		grade.Reason = gradeErr.Error()
	}
	path := filepath.Join(checkDir, referenceCheckRecordName)
	previous, _, err := readReferenceCheck(path)
	if err != nil {
		return errors.Join(gradeErr, err)
	}
	check := referenceCheck{
		AcceptanceDigest: grade.AcceptanceDigest, Grader: grader.agentSpec(), ReferenceRange: manifest.ReferenceRange,
		Grade: grade, Accepted: carryAccepted(previous.Accepted, grade.Scores, behaviours),
	}
	data, err := json.MarshalIndent(check, "", "  ")
	if err != nil {
		return errors.Join(gradeErr, err)
	}
	if err := os.MkdirAll(checkDir, 0o755); err != nil {
		return errors.Join(gradeErr, err)
	}
	if err := tasks.WriteAtomic(path, append(data, '\n'), 0o644); err != nil {
		return errors.Join(gradeErr, err)
	}
	fmt.Println(path)
	progress.line("%s finished: %s unaccepted=%v result=%s", label, gradeSummary(trialRecord{Grade: grade}), unacceptedFailures(check, behaviours), path)
	if gradeErr != nil {
		return fmt.Errorf("%s phase grading: %w", label, gradeErr)
	}
	return nil
}

// referencePatch reads the Reference diff as a patch that applies to the Case's
// parent commit, the same shape as a Trial's saved diff.
func referencePatch(manifest caseManifest, work string) ([]byte, error) {
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(work, manifest.Name+"-reference-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	clone := filepath.Join(dir, "repository")
	if err := cloneAtCommit(manifest.RepositoryURL, manifest.ParentCommit, clone); err != nil {
		return nil, err
	}
	patch, err := exec.Command("git", "-C", clone, "diff", "--binary", "--no-ext-diff", "--no-textconv", manifest.ReferenceRange, "--").Output()
	if err != nil {
		return nil, fmt.Errorf("read Reference diff %s: %w", manifest.ReferenceRange, err)
	}
	return patch, nil
}

func readReferenceCheck(path string) (referenceCheck, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return referenceCheck{}, false, nil
	}
	if err != nil {
		return referenceCheck{}, false, err
	}
	var check referenceCheck
	if err := json.Unmarshal(data, &check); err != nil {
		return referenceCheck{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return check, true, nil
}

// carryAccepted keeps each earlier reason whose behaviour the Reference still
// fails, under that behaviour's new number. A reason for a rewritten item or
// for an item the Reference now meets is dropped.
func carryAccepted(previous []acceptedFailure, scores *gradeScores, behaviours []string) []acceptedFailure {
	carried := []acceptedFailure{}
	if scores == nil {
		return carried
	}
	for _, item := range scores.Items {
		if item.Met {
			continue
		}
		for _, accepted := range previous {
			if accepted.Behaviour == behaviours[item.Item-1] && strings.TrimSpace(accepted.Reason) != "" {
				carried = append(carried, acceptedFailure{Item: item.Item, Behaviour: accepted.Behaviour, Reason: accepted.Reason})
				break
			}
		}
	}
	return carried
}

// unacceptedFailures lists the items that the Reference fails and that have no
// accepted reason for the same behaviour at the same number.
func unacceptedFailures(check referenceCheck, behaviours []string) []int {
	failed := []int{}
	if check.Grade == nil || check.Grade.Scores == nil {
		return failed
	}
	for _, item := range check.Grade.Scores.Items {
		if item.Met {
			continue
		}
		accepted := false
		for _, entry := range check.Accepted {
			if entry.Item == item.Item && item.Item <= len(behaviours) && entry.Behaviour == behaviours[item.Item-1] && strings.TrimSpace(entry.Reason) != "" {
				accepted = true
				break
			}
		}
		if !accepted {
			failed = append(failed, item.Item)
		}
	}
	return failed
}

// checkReferenceCheck says why a Case's Reference check does not yet allow its
// Acceptance list to be used, or nil when the check is current and every item
// the Reference fails has an accepted reason.
func checkReferenceCheck(caseDir, acceptance string, grader armFile) error {
	manifest, err := readCaseManifest(caseDir)
	if err != nil {
		return err
	}
	path := filepath.Join(caseDir, referenceCheckDirName, referenceCheckRecordName)
	rerun := "run go run ./eval reference-check " + manifest.Name
	check, exists, err := readReferenceCheck(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Case has no Reference check at %s; %s", path, rerun)
	}
	if check.AcceptanceDigest != acceptanceDigest(acceptance) || check.Grader != grader.agentSpec() || check.ReferenceRange != manifest.ReferenceRange {
		return fmt.Errorf("Reference check at %s is stale: the Acceptance list, the Grader or the Reference range changed; %s", path, rerun)
	}
	if check.Grade == nil || check.Grade.Status != "graded" || check.Grade.Scores == nil {
		status, reason := "missing", ""
		if check.Grade != nil {
			status, reason = check.Grade.Status, check.Grade.Reason
		}
		return fmt.Errorf("Reference check at %s did not grade the Reference: grade=%s %s", path, status, reason)
	}
	if failed := unacceptedFailures(check, acceptanceBehaviours(acceptance)); len(failed) > 0 {
		return fmt.Errorf("the Reference fails Acceptance items %v and %s gives no accepted reason for them; fix each item or accept it with a reason", failed, path)
	}
	return nil
}
