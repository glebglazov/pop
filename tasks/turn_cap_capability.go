package tasks

import (
	"encoding/json"
	"strconv"
	"strings"
)

// specTokens returns the argv tokens that tell this preset's CLI to stop after
// limit Turns, and nothing at all when the repository declares no cap or the
// adapter cannot be told about one — pop never claims a bound it cannot put on
// the command line (ADR-0190).
func (c AgentTurnCapEnforcementCapability) specTokens(limit int) []string {
	if limit <= 0 || c.Kind != CapabilitySupported || c.SpecTokens == nil {
		return nil
	}
	return c.SpecTokens(limit)
}

// argsContainTurnCap reports whether an augmented spec's own arguments already
// set this preset's cap, in which case the human's number wins and pop emits
// none of its own.
func (c AgentTurnCapEnforcementCapability) argsContainTurnCap(args []string) bool {
	if c.Contains != nil {
		return c.Contains(args)
	}
	return false
}

func claudeTurnCapSpecTokens(limit int) []string {
	return []string{"--max-turns", strconv.Itoa(limit)}
}

func claudeArgsContainTurnCap(args []string) bool {
	for _, arg := range args {
		if arg == "--max-turns" || strings.HasPrefix(arg, "--max-turns=") {
			return true
		}
	}
	return false
}

// The two fields claude's terminal record spells a capped ending with.
const (
	claudeMaxTurnsSubtype        = "error_max_turns"
	claudeMaxTurnsTerminalReason = "max_turns"
)

// claudeTurnCapExhausted is claude's Turn-cap exhaustion rule (ADR-0190,
// ADR-0165).
//
// Authoritative event: the terminal `result` record, which on a capped ending
// carries subtype "error_max_turns" and terminal_reason "max_turns" (measured
// 2026-08-06 from a live capped run, which also exits non-zero). Both halves are
// required: the exit status alone is every crash, and a stream that reports the
// ending without the process agreeing is a truncated capture, not an exhausted
// run.
//
// The run's own num_turns is deliberately not read here. It is claude's count,
// which sits one above the cap it enforced, and pop's Turn is pop's own
// measurement (ADR-0190 decision 7) — recognition never infers the ending from
// any count.
func claudeTurnCapExhausted(events []streamEventRecord, exitCode int) bool {
	if exitCode == 0 {
		return false
	}
	for _, ev := range events {
		var event struct {
			Type           string `json:"type"`
			Subtype        string `json:"subtype"`
			TerminalReason string `json:"terminal_reason"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "result" {
			continue
		}
		if event.Subtype == claudeMaxTurnsSubtype || event.TerminalReason == claudeMaxTurnsTerminalReason {
			return true
		}
	}
	return false
}

// attemptExhaustedTurnCap applies the agent's declared Turn-cap exhaustion
// capability to one finished attempt. An adapter that is Blind — or one pop has
// never heard of — answers false, so an attempt is only ever recorded as
// cap-exhausted where a captured run proves the ending is legible.
func attemptExhaustedTurnCap(agent string, events []streamEventRecord, exitCode int) bool {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return false
	}
	capability := adapter.TurnCapExhaustionCapability()
	if capability.Kind != CapabilitySupported || capability.Exhausted == nil {
		return false
	}
	return capability.Exhausted(events, exitCode)
}
