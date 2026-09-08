package tasks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunCapturedAgentInvocationMatchesSpendLens(t *testing.T) {
	for _, agent := range []string{"claude", "pi", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			d := newTestDeps(t)
			events := loadTurnFixture(t, agent+".events.jsonl.gz")
			var raw strings.Builder
			for _, event := range events {
				raw.WriteString(event.Raw + "\n")
			}
			behavior := &fakeAgentBehavior{attempts: []attemptScript{{rawOutput: raw.String(), changeFile: "answer.txt", changeData: "done"}}}
			token := registerFakeAgent(t, behavior)
			opts := CapturedAgentOptions{
				AgentSpec:   agent + " --model requested-model " + token,
				Prompt:      "Implement this spec without a task wrapper.",
				RuntimePath: t.TempDir(), Timeout: time.Minute,
				DestinationDir: filepath.Join(t.TempDir(), "capture"),
			}
			got, err := RunCapturedAgentInvocation(d, opts)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(opts.DestinationDir)
			if err != nil || len(entries) != 2 {
				t.Fatalf("want only the Captured run pair: %v, %v", entries, err)
			}
			metaPath, _ := findRunFiles(t, opts.DestinationDir)
			run, err := loadCapturedRun(d, opts.DestinationDir, filepath.Base(metaPath))
			if err != nil {
				t.Fatal(err)
			}
			if got.RunID != run.meta.RunID || run.meta.Phase != spendPhaseEval || run.meta.RequestedAgent != opts.AgentSpec || got.Outcome != streamOutcomeCompleted || got.ExitCode != 0 || got.ProceedVerdict != nil {
				t.Fatalf("result = %+v, meta = %+v", got, run.meta)
			}
			if behavior.calls != 1 || !reflect.DeepEqual(fakeAgentPrompts(token), []string{opts.Prompt}) {
				t.Fatalf("calls = %d, prompts = %q", behavior.calls, fakeAgentPrompts(token))
			}
			if _, err := os.Stat(filepath.Join(opts.RuntimePath, "answer.txt")); err != nil {
				t.Fatalf("agent did not work in the requested directory: %v", err)
			}
			want, err := runSpend(run)
			if err != nil {
				t.Fatal(err)
			}
			lens, actual, err := loadRunSpend(d, opts.DestinationDir, run)
			if err != nil {
				t.Fatal(err)
			}
			priced := priceRunSpend(run, lens, loadRateTableForSpend(d), nil)
			if got.Spend != want || got.Spend != lens || got.ActualModel != actual || actual == "" || got.Notional != priced {
				t.Fatalf("result = %+v, spend = %+v, model = %q, priced = %+v", got, want, actual, priced)
			}
			if agent == "claude" && (got.Spend.Turns.Count != 8 || got.Spend.PeakInput.Tokens != 40038 || !got.Spend.Cost.HasCost || got.Spend.Cost.Dollars != 0.5713835) {
				t.Fatalf("claude fixture figures = %+v", got)
			}
			if agent == "pi" && (!got.Spend.Cost.HasCost || got.Notional.Cost != got.Spend.Cost) {
				t.Fatalf("reported cost lost: %+v", got)
			}
			if agent == "cursor" && (got.Spend.Cost.HasCost || got.Notional.Cost.HasCost || got.Spend.PeakInput.HasPeak) {
				t.Fatalf("blind cost or peak must be absent: %+v", got)
			}
		})
	}
}

func TestRunCapturedAgentInvocationBlindAndFailedRuns(t *testing.T) {
	for _, tc := range []struct {
		name, agent, raw, outcome string
		exit                      int
	}{
		{"blind", "opencode", `{"type":"text","part":{"text":"done"}}`, streamOutcomeCompleted, 0},
		{"crash", "claude", `{"type":"system","subtype":"init"}`, streamOutcomeFailed, 7},
		{"quota", "claude", "", streamOutcomeQuotaPaused, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDeps(t)
			if tc.name == "quota" {
				var raw strings.Builder
				for _, event := range loadTurnFixture(t, "claude-session-limit.events.jsonl.gz") {
					raw.WriteString(event.Raw + "\n")
				}
				tc.raw = raw.String()
			}
			token := registerFakeAgent(t, &fakeAgentBehavior{attempts: []attemptScript{{rawOutput: tc.raw + "\n", exitCode: tc.exit}}})
			opts := CapturedAgentOptions{AgentSpec: tc.agent + " " + token, Prompt: "do work", RuntimePath: t.TempDir(), Timeout: time.Minute, DestinationDir: t.TempDir()}
			got, err := RunCapturedAgentInvocation(d, opts)
			if err != nil {
				t.Fatal(err)
			}
			meta, _ := findRunFiles(t, opts.DestinationDir)
			stored := readCapturedRunMeta(t, meta)
			if got.Outcome != tc.outcome || got.ExitCode != tc.exit || stored.Outcome != got.Outcome || stored.ExitCode != got.ExitCode {
				t.Fatalf("result = %+v, meta = %+v", got, stored)
			}
			if tc.name == "blind" && (got.Spend != (RunSpend{}) || got.Notional.Cost.HasCost || got.ActualModel != "") {
				t.Fatalf("blind figures must be absent: %+v", got)
			}
			if tc.name == "quota" && (got.ProceedVerdict == nil || got.ProceedVerdict.Kind != ProceedQuotaPause || got.ProceedVerdict.Preset != "claude" || got.ProceedVerdict.ResetAt.IsZero()) {
				t.Fatalf("quota verdict = %+v", got.ProceedVerdict)
			}
		})
	}
}

func TestRunCapturedAgentInvocationRefusesUncapturedSpecs(t *testing.T) {
	for _, tc := range []struct {
		spec string
		mode AgentOutputMode
		why  string
	}{
		{"sh -c true", AgentOutputAuto, "custom commands file no Captured run"},
		{"claude", AgentOutputText, "plain-output specs file no Captured run"},
	} {
		opts := CapturedAgentOptions{AgentSpec: tc.spec, OutputMode: tc.mode, RuntimePath: t.TempDir(), Timeout: time.Minute, DestinationDir: t.TempDir()}
		_, err := RunCapturedAgentInvocation(newTestDeps(t), opts)
		if err == nil || !strings.Contains(err.Error(), tc.why) {
			t.Fatalf("spec %q: %v", tc.spec, err)
		}
		entries, err := os.ReadDir(opts.DestinationDir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("refused invocation wrote files: %v, %v", entries, err)
		}
	}
}

func TestRunCapturedAgentInvocationReportsCaptureFailure(t *testing.T) {
	d := newTestDeps(t)
	token := registerFakeAgent(t, &fakeAgentBehavior{attempts: []attemptScript{{rawOutput: "{}\n"}}})
	dest := filepath.Join(t.TempDir(), "file")
	writeFile(t, dest, "occupied")
	_, err := RunCapturedAgentInvocation(d, CapturedAgentOptions{AgentSpec: "claude " + token, RuntimePath: t.TempDir(), Timeout: time.Minute, DestinationDir: dest})
	if err == nil || !strings.Contains(err.Error(), "write Captured run") {
		t.Fatalf("capture failure = %v", err)
	}
}

func TestRunCapturedAgentInvocationPricesUnreportedCost(t *testing.T) {
	for _, declared := range []bool{false, true} {
		d := newTestDeps(t)
		token := registerFakeAgent(t, &fakeAgentBehavior{attempts: []attemptScript{{rawOutput: `{"type":"system","subtype":"init","model":"claude-opus-5"}
{"type":"result","subtype":"success","result":"done","usage":{"input_tokens":100,"output_tokens":50}}
`}}})
		opts := CapturedAgentOptions{AgentSpec: "claude " + token, RuntimePath: t.TempDir(), Timeout: time.Minute, DestinationDir: t.TempDir()}
		source := RateSourceTable
		if declared {
			opts.DeclaredRates = map[string]ModelRates{"anthropic/claude-opus-5": {Prompt: 0.001, Completion: 0.002}}
			source = RateSourceOverride
		}
		got, err := RunCapturedAgentInvocation(d, opts)
		if err != nil {
			t.Fatal(err)
		}
		if got.Spend.Cost.HasCost || !got.Notional.Cost.HasCost || got.Notional.Cost.Dollars <= 0 || got.Notional.RateSource != source || got.Notional.ModelKeySource != RateKeyFromActual {
			t.Fatalf("pricing = %+v", got)
		}
		if declared && got.Notional.Cost.Dollars != 0.2 {
			t.Fatalf("declared price = %+v", got.Notional)
		}
	}
}

func TestRunCapturedAgentInvocationPersistsTimeout(t *testing.T) {
	d := newTestDeps(t)
	root := t.TempDir()
	installClaudeHangingAgent(t, root, false)
	opts := CapturedAgentOptions{AgentSpec: "claude", RuntimePath: root, Timeout: 100 * time.Millisecond, DestinationDir: t.TempDir()}
	got, err := RunCapturedAgentInvocation(d, opts)
	if err != nil {
		t.Fatal(err)
	}
	metaPath, _ := findRunFiles(t, opts.DestinationDir)
	run, err := loadCapturedRun(d, opts.DestinationDir, filepath.Base(metaPath))
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != streamOutcomeTimedOut || run.meta.Outcome != got.Outcome || run.meta.ExitCode != got.ExitCode {
		t.Fatalf("timeout result = %+v, meta = %+v", got, run.meta)
	}
}
