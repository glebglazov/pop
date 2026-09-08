package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks"
)

func TestRunMatrixIsRepeatMajorAndResumes(t *testing.T) {
	results := t.TempDir()
	var calls, grades []string
	attempts := map[string]int{}
	runner := func(caseName, arm string, repeat int) (string, error) {
		key := fmt.Sprintf("%d/%s/%s", repeat, caseName, arm)
		calls = append(calls, key)
		attempts[key]++
		outcome := "completed"
		if key == "1/case-a/bare" {
			outcome = "invalid"
		}
		dir := filepath.Join(results, caseName, arm, fmt.Sprintf("%02d", repeat))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		record := trialRecord{Case: caseName, Arm: arm, Repeat: repeat, Attempts: attempts[key], Outcome: outcome}
		data, _ := json.Marshal(record)
		if err := tasks.WriteAtomic(filepath.Join(dir, "trial.json"), data, 0o644); err != nil {
			return "", err
		}
		return outcome, nil
	}
	grader := func(caseName, arm string, repeat int) error {
		grades = append(grades, fmt.Sprintf("%d/%s/%s", repeat, caseName, arm))
		path := filepath.Join(results, caseName, arm, fmt.Sprintf("%02d", repeat), "trial.json")
		record, _, err := readTrialRecord(path)
		if err != nil {
			return err
		}
		record.Grade = &gradeRecord{Status: "graded"}
		data, _ := json.Marshal(record)
		return tasks.WriteAtomic(path, data, 0o644)
	}
	cases, arms, repeats := []string{"case-a", "case-b"}, []string{"bare", "pop"}, []int{1, 2}
	if err := runMatrix(cases, arms, repeats, results, runner, grader); err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{
		"1/case-a/bare", "1/case-a/bare", "1/case-a/pop", "1/case-b/bare", "1/case-b/pop",
		"2/case-a/bare", "2/case-a/pop", "2/case-b/bare", "2/case-b/pop",
	}
	if strings.Join(calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("Trial order = %v; want %v", calls, wantCalls)
	}
	if attempts["1/case-a/bare"] != 2 || len(grades) != 8 || grades[0] != "1/case-a/bare" {
		t.Fatalf("attempts = %v; grades = %v", attempts, grades)
	}
	retried, _, err := readTrialRecord(filepath.Join(results, "case-a", "bare", "01", "trial.json"))
	if err != nil || retried.Attempts != 2 || retried.Outcome != "invalid" {
		t.Fatalf("retried Trial = %+v, %v", retried, err)
	}
	calls, grades = nil, nil
	if err := runMatrix(cases, arms, repeats, results, runner, grader); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 || len(grades) != 0 {
		t.Fatalf("resume reran Trials: calls=%v grades=%v", calls, grades)
	}
}

func TestSelectCasesDefaultsToEveryApprovedCase(t *testing.T) {
	root := t.TempDir()
	for _, fixture := range []struct {
		name, status string
	}{{"approved-a", "approved"}, {"draft", "draft"}, {"approved-b", "approved"}} {
		dir := filepath.Join(root, fixture.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest, _ := json.Marshal(caseManifest{Name: fixture.name, RepositoryURL: "https://example.test/repo.git", ParentCommit: "abc", ReferenceRange: "abc..def"})
		writeFile(t, filepath.Join(dir, caseManifestName), string(manifest))
		writeFile(t, filepath.Join(dir, acceptanceName), "Status: "+fixture.status+"\n")
	}
	selected, err := selectCases(root, nil)
	if err != nil || strings.Join(selected, ",") != "approved-a,approved-b" {
		t.Fatalf("selected Cases = %v, %v", selected, err)
	}
	if _, err := selectCases(root, []string{"draft"}); err == nil {
		t.Fatal("explicit draft Case was accepted")
	}
}

func TestEvalRollupFiguresCountsAndBlindValues(t *testing.T) {
	root := t.TempDir()
	start := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	known := func(repeat int, tokens int64, cost float64, turns int, peak int64, seconds int, grade *gradeRecord, outcome string) trialRecord {
		return trialRecord{
			Case: "case-a", Arm: "bare", Repeat: repeat, Outcome: outcome,
			StartedAt: start, EndedAt: start.Add(time.Duration(seconds) * time.Second), Grade: grade,
			Spend: tasks.RunSpend{
				Tokens:    tasks.TokenUsage{Input: tokens, HasInput: true},
				Turns:     tasks.TurnCount{Count: turns, HasTurn: true},
				PeakInput: tasks.PeakInput{Tokens: peak, HasPeak: true},
			},
			Notional: tasks.PricedSpend{Cost: tasks.PartialCost{Dollars: cost, HasCost: true}},
		}
	}
	graded := func(status string, ratio float64, quality int) *gradeRecord {
		return &gradeRecord{Status: status, Scores: &gradeScores{ListRatio: ratio, Quality: quality}}
	}
	records := []trialRecord{
		known(1, 100, 1, 2, 50, 10, graded("graded", 0.5, 3), "completed"),
		known(2, 300, 3, 6, 150, 30, graded("timed_out", 0, 0), "timed_out"),
		{Case: "case-a", Arm: "bare", Repeat: 3, Outcome: "completed", StartedAt: start, EndedAt: start.Add(20 * time.Second), Grade: &gradeRecord{Status: "gate_failed", Scores: &gradeScores{}}},
		{Case: "case-a", Arm: "bare", Repeat: 4, Outcome: "invalid", Grade: &gradeRecord{Status: "ungraded"}},
		{Case: "case-b", Arm: "pop", Repeat: 1, Outcome: "completed", StartedAt: start, EndedAt: start.Add(time.Second)},
	}
	for _, record := range records {
		dir := filepath.Join(root, record.Case, record.Arm, fmt.Sprintf("%02d", record.Repeat))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(record)
		writeFile(t, filepath.Join(dir, "trial.json"), string(data))
	}
	rollup, err := loadEvalRollup(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rollup.Rows) != 2 {
		t.Fatalf("rows = %+v", rollup.Rows)
	}
	row := rollup.Rows[0]
	assertMetric(t, "tokens", row.TotalTokens, 200, 200, 1)
	assertMetric(t, "cost", row.NotionalCostUSD, 2, 2, 1)
	assertMetric(t, "turns", row.Turns, 4, 4, 1)
	assertMetric(t, "peak", row.PeakInputTokens, 100, 100, 1)
	assertMetric(t, "wall", row.WallClockSeconds, 20, 20, 0)
	if row.Trials != 4 || row.Counted != 3 || row.GateFailures != 1 || row.Timeouts != 1 || row.Invalid != 1 || row.Ungraded != 1 || row.AcceptanceRatio == nil || *row.AcceptanceRatio != 0 || row.QualityScore == nil || *row.QualityScore != 0 {
		t.Fatalf("row = %+v", row)
	}
	blind := rollup.Rows[1]
	if blind.TotalTokens.Median != nil || blind.TotalTokens.Blind != 1 || blind.Ungraded != 1 {
		t.Fatalf("blind row = %+v", blind)
	}
	var human strings.Builder
	renderEvalRollup(&human, rollup)
	if !strings.Contains(human.String(), "200/200") || !strings.Contains(human.String(), "—") || !strings.Contains(human.String(), "1/1/1/1/0") {
		t.Fatalf("human Rollup:\n%s", human.String())
	}
	data, err := json.Marshal(rollup)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"median":200`) || !strings.Contains(string(data), `"blind":1`) {
		t.Fatalf("JSON Rollup: %s", data)
	}
	var machine bytes.Buffer
	if err := renderEvalRollupJSON(&machine, rollup); err != nil {
		t.Fatal(err)
	}
	var decoded evalRollup
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil || len(decoded.Rows) != len(rollup.Rows) || *decoded.Rows[0].TotalTokens.Median != 200 {
		t.Fatalf("machine Rollup = %+v, %v", decoded, err)
	}
}

func assertMetric(t *testing.T, name string, got metricRollup, median, spread float64, blind int) {
	t.Helper()
	if got.Median == nil || *got.Median != median || got.Spread == nil || *got.Spread != spread || got.Blind != blind {
		t.Fatalf("%s = %+v", name, got)
	}
}

func TestPopTrialFiguresUseSpendLensTotalsAndRows(t *testing.T) {
	record := trialRecord{Case: "case", Arm: "pop", Repeat: 1, SetSpend: json.RawMessage(`{
  "implement_input_tokens": 100,
  "implement_output_tokens": 20,
  "implement_cache_read_tokens": 30,
  "implement_run_count": 2,
  "implement_notional_cost_usd": 1.25,
  "verification_input_tokens": 40,
  "verification_output_tokens": 10,
  "verification_run_count": 1,
  "verification_notional_cost_usd": 0.75,
  "rows": [
    {"turns": 3, "turn_blind_runs": 0, "peak_input_tokens": 80, "peak_blind_runs": 0},
    {"turns": 2, "turn_blind_runs": 0, "peak_input_tokens": 120, "peak_blind_runs": 0}
  ]
}`)}
	figures, err := trialFigures(record)
	if err != nil {
		t.Fatal(err)
	}
	if figures.tokens == nil || *figures.tokens != 200 || figures.cost == nil || *figures.cost != 2 || figures.turns == nil || *figures.turns != 5 || figures.peak == nil || *figures.peak != 120 {
		t.Fatalf("Pop figures = %+v", figures)
	}
	record.SetSpend = json.RawMessage(`{"implement_input_tokens":100,"implement_run_count":2,"implement_token_blind_runs":1,"rows":[{"turns":3,"turn_blind_runs":1,"peak_blind_runs":1}]}`)
	figures, err = trialFigures(record)
	if err != nil || figures.tokens != nil || figures.cost != nil || figures.turns != nil || figures.peak != nil {
		t.Fatalf("blind Pop figures = %+v, %v", figures, err)
	}
}
