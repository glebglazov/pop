package tasks

import (
	"bufio"
	"encoding/json"
	"strings"
	"time"
)

// Kimi quota signals: the stable stderr substrings that gate Agent quota
// detection for the kimi preset. kimi writes quota diagnostics to stderr and
// never into its stream-json, and the texts carry no reset hint, so each signal
// maps to a fixed backoff plus the shared quota assurance offset (ADR-0151).
// Transient overload and concurrency 429s are deliberately absent: kimi retries
// those internally and reports them as informational meta retry lines.
const (
	kimiPeriodQuotaSignal       = "usage limit for this period"
	kimiBillingCycleQuotaSignal = "usage limit for this billing cycle"
	kimiMonthlyQuotaSignal      = "monthly usage limit"

	kimiPeriodQuotaBackoff       = time.Hour
	kimiBillingCycleQuotaBackoff = 24 * time.Hour
	kimiMonthlyQuotaBackoff      = 7 * 24 * time.Hour
)

// kimiQuotaPauseReason scans the raw agent capture line-by-line and returns a
// quota-pause Agent unavailability when any line carries a kimi quota signal.
// The whole matching line becomes the reason so the human sees kimi's own
// wording and the reset derivation can re-read the signal from it.
func kimiQuotaPauseReason(raw string) *AgentUnavailability {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if kimiQuotaBackoff(line) > 0 {
			return DetectedQuotaPause(line)
		}
	}
	return nil
}

// kimiQuotaResetAt derives PauseResetAt from a kimi quota diagnostic: the
// signal's fixed backoff plus the assurance offset. A diagnostic carrying no
// signal yields the zero time, leaving the pause without a reset instant.
func kimiQuotaResetAt(reason string, now time.Time) time.Time {
	backoff := kimiQuotaBackoff(reason)
	if backoff == 0 {
		return time.Time{}
	}
	return now.Add(backoff).Add(quotaAssuranceOffset)
}

// kimiQuotaBackoff returns the backoff a diagnostic's quota signal calls for,
// longest window first so a message naming two windows waits out the wider one.
func kimiQuotaBackoff(diagnostic string) time.Duration {
	lower := strings.ToLower(diagnostic)
	switch {
	case strings.Contains(lower, kimiMonthlyQuotaSignal):
		return kimiMonthlyQuotaBackoff
	case strings.Contains(lower, kimiBillingCycleQuotaSignal):
		return kimiBillingCycleQuotaBackoff
	case strings.Contains(lower, kimiPeriodQuotaSignal):
		return kimiPeriodQuotaBackoff
	default:
		return 0
	}
}

// kimiStreamLine is one line of kimi's stream-json: an OpenAI-shaped message
// whose role discriminates it. assistant lines carry prose, tool calls, or
// both; tool lines carry one call's result; meta lines are kimi's own framing
// (session resume hints, version banners, retry notices). There is no init or
// result event — failure is the exit code plus stderr.
type kimiStreamLine struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		Function struct {
			Name string `json:"name"`
			// Arguments is a JSON object encoded as a string, as in the
			// OpenAI tool-call wire shape.
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// normalizeKimiStreamJSON recovers the completion text from the last assistant
// line that carries prose. Tool-call-only assistant lines are skipped: kimi
// flushes one whenever a step ends in tool calls, and treating it as the
// transcript would blank a completed turn. kimi emits no result event, so a
// failed run has no assistant prose and the raw capture (exit code plus
// stderr) remains what the completion contract sees.
func normalizeKimiStreamJSON(raw string) AgentResult {
	if pause := kimiQuotaPauseReason(raw); pause != nil {
		return AgentResult{Unavailability: pause}
	}
	var transcript string
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var msg kimiStreamLine
		if err := json.Unmarshal(line, &msg); err != nil {
			return false
		}
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			transcript = msg.Content
		}
		return true
	})
	return normalizedTranscript(transcript, nil)
}

// kimiLineRenderer renders kimi stream-json lines live: an assistant line emits
// its prose plus a dim "→ Tool hint" tick per tool call it requested, in that
// order (kimi's own flush order). Tool results and meta lines render nothing —
// meta covers the retry notices kimi emits while it retries a transient
// provider error internally, which are informational rather than failures.
// Thinking never reaches this stream at all, so there is nothing to suppress.
// Non-JSON lines are reported as unhandled so the writer passes them through
// raw; kimi's Bash tool echoes command output to stderr, which lands here.
func kimiLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	return func(line []byte) (string, bool) {
		var msg kimiStreamLine
		if err := json.Unmarshal(line, &msg); err != nil {
			return "", false
		}
		if msg.Role != "assistant" {
			return "", true
		}
		var out strings.Builder
		if text := strings.TrimRight(msg.Content, "\n"); text != "" {
			out.WriteString(text + "\n")
		}
		for _, call := range msg.ToolCalls {
			out.WriteString(dim(kimiToolTick(call.Function.Name, call.Function.Arguments)) + "\n")
		}
		return out.String(), true
	}
}

// kimiToolTick formats a compact "→ Name hint" line from a tool call whose
// arguments arrive as an encoded JSON string.
func kimiToolTick(name, arguments string) string {
	return toolTick(name, kimiToolHint(arguments))
}

type kimiToolHintProbe struct {
	Path    string `json:"path"`
	Command string `json:"command"`
	Pattern string `json:"pattern"`
	Query   string `json:"query"`
	URL     string `json:"url"`
}

// kimiToolHint probes a tool call's arguments for the first salient value.
// Every kimi file tool names its target `path` (never file_path), Bash uses
// `command`, Grep and Glob use `pattern`, and the web tools use `query`/`url`.
// Arguments stream in as a partially built string on a tool-call delta, so
// unparseable JSON yields no hint rather than a fabricated one.
func kimiToolHint(arguments string) string {
	if strings.TrimSpace(arguments) == "" {
		return ""
	}
	return toolHint(json.RawMessage(arguments), func(p kimiToolHintProbe) string {
		return firstNonEmpty(p.Path, p.Command, p.Pattern, p.Query, p.URL)
	})
}
