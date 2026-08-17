package tasks

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// agentGroupCfg builds a config whose implement list is the fallthrough target
// and whose verify and review groups state no list of their own. The named keys
// are the ones the override layer states as an empty list — the state
// applyConfigLayerMerge records on a config loaded from disk.
func agentGroupCfg(emptyOverrides ...string) *config.Config {
	return &config.Config{
		Work: &config.WorkConfig{
			Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands("cursor", "claude")},
		},
		EmptyAgentOverrides: emptyOverrides,
	}
}

// TestResolveVerifierEmptyOverrideDisablesFallthrough pins the two empty states
// apart for the Verifier: an absent [work.verify].agents walks on to the
// implement list, an override of `agents = []` refuses (ADR-0202 decision 6).
func TestResolveVerifierEmptyOverrideDisablesFallthrough(t *testing.T) {
	m := manifestWithVerifier(t, nil, "")

	sel, err := resolveVerifier(nil, "", m, agentGroupCfg())
	if err != nil {
		t.Fatalf("resolveVerifier with an absent list: %v", err)
	}
	if got := strings.Join(sel.Agents, ","); got != "cursor,claude" {
		t.Fatalf("agents = %v, want the implement list", sel.Agents)
	}

	_, err = resolveVerifier(nil, "", m, agentGroupCfg(config.KeyVerifyAgents))
	if err == nil {
		t.Fatal("resolveVerifier resolved an explicit empty override; want a refusal")
	}
	assertExitCode(t, err, ExitSetup)
	if !strings.Contains(err.Error(), config.KeyVerifyAgents) {
		t.Fatalf("error = %q, want it to name %s", err, config.KeyVerifyAgents)
	}

	// A human naming an agent on the command line is not refused: the empty
	// override disables the fallthrough, not the run.
	sel, err = resolveVerifier([]string{"codex"}, "", m, agentGroupCfg(config.KeyVerifyAgents))
	if err != nil {
		t.Fatalf("resolveVerifier with --agent: %v", err)
	}
	if got := strings.Join(sel.Agents, ","); got != "codex" {
		t.Fatalf("agents = %v, want the CLI list", sel.Agents)
	}

	// So is the set's own verifier directive, which outranks config the same way.
	sel, err = resolveVerifier(nil, "", manifestWithVerifier(t, []string{"pi"}, ""), agentGroupCfg(config.KeyVerifyAgents))
	if err != nil {
		t.Fatalf("resolveVerifier with a per-set override: %v", err)
	}
	if got := strings.Join(sel.Agents, ","); got != "pi" {
		t.Fatalf("agents = %v, want the per-set list", sel.Agents)
	}
}

// TestResolveReviewerEmptyOverrideDisablesFallthrough is the same pair for the
// Reviewer, which reads its own key through the one shared rule.
func TestResolveReviewerEmptyOverrideDisablesFallthrough(t *testing.T) {
	sel, err := resolveReviewer(nil, "", agentGroupCfg())
	if err != nil {
		t.Fatalf("resolveReviewer with an absent list: %v", err)
	}
	if got := strings.Join(sel.Agents, ","); got != "cursor,claude" {
		t.Fatalf("agents = %v, want the implement list", sel.Agents)
	}

	_, err = resolveReviewer(nil, "", agentGroupCfg(config.KeyReviewAgents))
	if err == nil {
		t.Fatal("resolveReviewer resolved an explicit empty override; want a refusal")
	}
	assertExitCode(t, err, ExitSetup)
	if !strings.Contains(err.Error(), config.KeyReviewAgents) {
		t.Fatalf("error = %q, want it to name %s", err, config.KeyReviewAgents)
	}

	sel, err = resolveReviewer([]string{"codex"}, "", agentGroupCfg(config.KeyReviewAgents))
	if err != nil {
		t.Fatalf("resolveReviewer with --agent: %v", err)
	}
	if got := strings.Join(sel.Agents, ","); got != "codex" {
		t.Fatalf("agents = %v, want the CLI list", sel.Agents)
	}
}

// TestEmptyOverrideOfOneGroupLeavesTheOthers pins that the refusal is keyed to
// the group whose list a human emptied: silencing verify says nothing about
// review, and neither touches implement's own list.
func TestEmptyOverrideOfOneGroupLeavesTheOthers(t *testing.T) {
	cfg := agentGroupCfg(config.KeyVerifyAgents)

	if _, err := resolveVerifier(nil, "", manifestWithVerifier(t, nil, ""), cfg); err == nil {
		t.Fatal("verify resolved an explicit empty override")
	}
	sel, err := resolveReviewer(nil, "", cfg)
	if err != nil {
		t.Fatalf("review refused on verify's override: %v", err)
	}
	if got := strings.Join(sel.Agents, ","); got != "cursor,claude" {
		t.Fatalf("review agents = %v, want the implement list", sel.Agents)
	}
}
