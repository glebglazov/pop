package tasks

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// The minutes are optional because claude states the hour alone when the
	// window lands on one — "resets 9pm", observed 2026-08-24. Requiring them was
	// what left that refusal undated (ADR-0233).
	claudeWeekdayResetAtPattern = regexp.MustCompile(`(?i)\bresets\s+(Sun|Mon|Tue|Wed|Thu|Fri|Sat)\s+([0-9]{1,2})(?::([0-9]{2}))?\s*([AP]M)\b`)
	claudeBareResetAtPattern    = regexp.MustCompile(`(?i)\bresets\s+([0-9]{1,2})(?::([0-9]{2}))?\s*([AP]M)\b`)
	// The zone claude names after the clock — "resets 9pm (Europe/Madrid)". An
	// hour with no zone is ambiguous, so the message states one; reading it is
	// what keeps a machine in another zone from waiting out the wrong hour.
	claudeResetZonePattern = regexp.MustCompile(`\(([A-Za-z_]+/[A-Za-z0-9_+\-/]+)\)`)
)

func normalizeClaudeStreamJSON(raw string) AgentResult {
	return normalizeResultStreamJSON(raw, func(result string) *AgentProceedVerdict {
		v := claudeQuotaPauseReason(raw, result)
		if v == nil {
			return nil
		}
		// The instant is resolved here, where the whole capture is in hand,
		// rather than at the executor's reason-only seam: claude states the
		// reset as an epoch on its own rate-limit event, and only this call
		// site can see it (ADR-0233).
		stamped := v.WithResetAt(claudeStreamQuotaResetAt(raw, v.Reason, time.Now()))
		return &stamped
	})
}

// claudeLineRenderer renders claude stream-json events live: assistant prose
// plain, a dim "model <id>" line when the init event reports the actual model,
// and a dim "→ Tool hint" tick per tool use. Other event types render nothing;
// non-JSON lines are reported as unhandled so the writer passes them through
// raw.
func claudeLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	printedModel := false
	return func(line []byte) (string, bool) {
		var event struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Model   string `json:"model"`
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Text  string          `json:"text"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", false
		}
		if event.Type == "system" && event.Subtype == "init" {
			model := strings.TrimSpace(event.Model)
			if model == "" || printedModel {
				return "", true
			}
			printedModel = true
			return dim("model "+model) + "\n", true
		}
		if event.Type != "assistant" {
			return "", true
		}
		var b strings.Builder
		for _, c := range event.Message.Content {
			switch c.Type {
			case "text":
				if text := strings.TrimRight(c.Text, "\n"); text != "" {
					b.WriteString(text)
					b.WriteByte('\n')
				}
			case "tool_use":
				b.WriteString(dim(claudeToolTick(c.Name, c.Input)))
				b.WriteByte('\n')
			}
		}
		return b.String(), true
	}
}

func claudeActualModel(events []streamEventRecord) string {
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Model   string `json:"model"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type == "system" && event.Subtype == "init" {
			return strings.TrimSpace(event.Model)
		}
	}
	return ""
}

// claudeTokenUsage is claude's Usage extraction rule (ADR-0160).
//
// Authoritative events: each per-API-call `usage` block Claude emits for an
// assistant turn. In the stream-json wire format those blocks are aggregated
// onto events with a top-level `usage` object under Anthropic API field names
// (input_tokens, output_tokens, cache_read_input_tokens,
// cache_creation_input_tokens) — typically the terminal `result` event.
// Nested message.usage on streamed assistant partials is not authoritative
// (duplicated incomplete figures); other shapes (e.g. task_progress) lack the
// API fields and are ignored.
//
// Semantics: accumulate — sum every matching block. A present zero is a
// reported value (Has* true), not a Token-blind absence.
func claudeTokenUsage(events []streamEventRecord) TokenUsage {
	var u TokenUsage
	for _, ev := range events {
		var usage struct {
			Usage struct {
				InputTokens              *int64 `json:"input_tokens"`
				OutputTokens             *int64 `json:"output_tokens"`
				CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
				CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &usage); err != nil {
			continue
		}
		if v := usage.Usage.InputTokens; v != nil {
			u.Input += *v
			u.HasInput = true
		}
		if v := usage.Usage.OutputTokens; v != nil {
			u.Output += *v
			u.HasOutput = true
		}
		if v := usage.Usage.CacheReadInputTokens; v != nil {
			u.CacheRead += *v
			u.HasCacheRead = true
		}
		if v := usage.Usage.CacheCreationInputTokens; v != nil {
			u.CacheWrite += *v
			u.HasCacheWrite = true
		}
	}
	return u
}

// claudePartialCost is claude's cost extraction rule (ADR-0160).
//
// Authoritative event: the terminal `result` event's total_cost_usd field and
// per-model costUSD entries in modelUsage. That block is the whole-run total
// — claude emits it once on the final result.
//
// Semantics: replace — read the last matching result; sum nothing.
func claudePartialCost(events []streamEventRecord) PartialCost {
	var c PartialCost
	for _, ev := range events {
		var event struct {
			Type         string   `json:"type"`
			TotalCostUSD *float64 `json:"total_cost_usd"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "result" || event.TotalCostUSD == nil {
			continue
		}
		c.Dollars = *event.TotalCostUSD
		c.HasCost = true
	}
	return c
}

// claudeTurnCount is claude's Turn extraction rule (ADR-0165).
//
// Authoritative events: type=="assistant", deduped by message.id. Consecutive
// assistant events repeat identical usage for the same turn; counting without
// dedup inflates the figure.
func claudeTurnCount(events []streamEventRecord) TurnCount {
	seen := make(map[string]struct{})
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "assistant" || event.Message.ID == "" {
			continue
		}
		seen[event.Message.ID] = struct{}{}
	}
	return TurnCount{Count: len(seen), HasTurn: true}
}

// claudePeakInput is claude's peak-input extraction rule (ADR-0165).
//
// For each deduped assistant turn, read message.usage and take the maximum of
// input_tokens + cache_read_input_tokens + cache_creation_input_tokens.
func claudePeakInput(events []streamEventRecord) PeakInput {
	perTurn := make(map[string]int64)
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Usage *struct {
					InputTokens              *int64 `json:"input_tokens"`
					CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
					CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "assistant" || event.Message.ID == "" || event.Message.Usage == nil {
			continue
		}
		usage := event.Message.Usage
		var sum int64
		if v := usage.InputTokens; v != nil {
			sum += *v
		}
		if v := usage.CacheReadInputTokens; v != nil {
			sum += *v
		}
		if v := usage.CacheCreationInputTokens; v != nil {
			sum += *v
		}
		perTurn[event.Message.ID] = sum
	}
	if len(perTurn) == 0 {
		return PeakInput{}
	}
	var peak int64
	for _, v := range perTurn {
		if v > peak {
			peak = v
		}
	}
	return PeakInput{Tokens: peak, HasPeak: true}
}

// claudeToolTick formats a compact "→ Name hint" line, probing the tool input
// for the first recognized salient key without knowing per-tool schemas.
func claudeToolTick(name string, input json.RawMessage) string {
	return toolTick(name, claudeToolHint(input))
}

type claudeToolHintProbe struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path"`
	Command  string `json:"command"`
	Pattern  string `json:"pattern"`
	URL      string `json:"url"`
	Query    string `json:"query"`
}

func claudeToolHint(input json.RawMessage) string {
	return toolHint(input, func(p claudeToolHintProbe) string {
		return firstNonEmpty(p.FilePath, p.Path, p.Command, p.Pattern, p.URL, p.Query)
	})
}

// claudeToolTimings derives per-tool durations from one stored Captured
// attempt stream: each assistant tool_use block is paired with the user
// tool_result block carrying the same tool-use id, and the gap between their
// arrival times is that invocation's duration. Ids — not order — do the
// pairing, so parallel tool calls within one assistant turn resolve correctly.
// A tool_use with no result (e.g. a killed attempt) contributes nothing to the
// per-tool rows, but its still-open interval is reported as a tool window so
// Model time never absorbs the wait on a tool that was running when the
// attempt ended. Results aggregate per tool name, longest total first.
func claudeToolTimings(events []streamEventRecord) ([]ToolTiming, []toolWindow) {
	return accumulateToolTimings(events, func(ev streamEventRecord) ([]toolOpen, []toolClose) {
		var msg struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type      string `json:"type"`
					ID        string `json:"id"`
					Name      string `json:"name"`
					ToolUseID string `json:"tool_use_id"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &msg); err != nil {
			return nil, nil
		}
		switch msg.Type {
		case "assistant":
			var opens []toolOpen
			for _, c := range msg.Message.Content {
				if c.Type == "tool_use" && c.ID != "" {
					opens = append(opens, toolOpen{ID: c.ID, Name: c.Name})
				}
			}
			return opens, nil
		case "user":
			var closes []toolClose
			for _, c := range msg.Message.Content {
				if c.Type == "tool_result" {
					closes = append(closes, toolClose{ID: c.ToolUseID})
				}
			}
			return nil, closes
		default:
			return nil, nil
		}
	})
}

// claudeRefusalSignature is how claude recognises a quota refusal: the typed
// fields of its own capture first, its three marker sentences beneath them
// (ADR-0234).
//
// The markers are demoted rather than deleted because they are the only reading
// left for a capture whose event schema changed instead of its wording — and
// because each sentence names the window its refusal exhausted, which is the
// class reading for a capture stating no rate-limit type.
var claudeRefusalSignature = AgentRefusalSignatureCapability{
	Kind:       CapabilitySupported,
	Structured: claudeStructuredRefusal,
	Markers: []AgentRefusalMarker{
		{Sentence: "You've hit your session limit", Class: QuotaWindowFiveHour},
		{Sentence: "You've hit your weekly limit", Class: QuotaWindowWeekly},
		{Sentence: "You've hit your Opus limit", Class: QuotaWindowOpus},
	},
}

func claudeQuotaPauseReason(raw, result string) *AgentProceedVerdict {
	return claudeRefusalSignature.detectRefusal(raw, result)
}

// claudeRefusalHTTPStatus is what claude reports as api_error_status on its
// terminal result when the provider turned the request away.
const claudeRefusalHTTPStatus = 429

// claudeStructuredRefusal reads a refusal out of the typed fields of one whole
// capture: the terminal result event's 429 together with a rate-limit event
// reporting a rejection (ADR-0234). It answers the Quota window class that event
// names, and whether the pair was there at all.
//
// The pair is the reading. A 429 on its own is a transient overload of an API
// that is still willing to serve this account, and a rejection on its own can be
// reported on an event pop is not otherwise acting on — the same event type
// carries `allowed` and `allowed_warning` readings all through a healthy run.
// The last rejection wins, as it does when the reset epoch is read.
func claudeStructuredRefusal(raw string) (AgentQuotaWindowClass, bool) {
	var refusedStatus, rejected bool
	class := QuotaWindowUnknown
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type           string `json:"type"`
			APIErrorStatus int    `json:"api_error_status"`
			RateLimit      struct {
				Status        string `json:"status"`
				RateLimitType string `json:"rateLimitType"`
			} `json:"rate_limit_info"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		switch event.Type {
		case "result":
			if event.APIErrorStatus == claudeRefusalHTTPStatus {
				refusedStatus = true
			}
		case "rate_limit_event":
			if event.RateLimit.Status == "rejected" {
				rejected = true
				class = claudeQuotaWindowClass(event.RateLimit.RateLimitType)
			}
		}
		return true
	})
	return class, refusedStatus && rejected
}

// claudeQuotaWindowClass maps claude's rateLimitType onto the window class pop
// holds. `five_hour` is the spelling the captured refusal states; the weekly
// windows are matched by substring because their wire spelling has not been
// captured, and a window pop cannot place is Unknown rather than a guess. Opus
// is tested first: a spelling that names both the Opus allowance and the week it
// runs over is the Opus one.
func claudeQuotaWindowClass(rateLimitType string) AgentQuotaWindowClass {
	token := strings.ToLower(strings.TrimSpace(rateLimitType))
	switch {
	case token == "":
		return QuotaWindowUnknown
	case token == "five_hour":
		return QuotaWindowFiveHour
	case strings.Contains(token, "opus"):
		return QuotaWindowOpus
	case strings.Contains(token, "week"), strings.Contains(token, "seven_day"):
		return QuotaWindowWeekly
	default:
		return QuotaWindowUnknown
	}
}

// claudeStreamQuotaResetAt dates a detected quota pause from the whole capture:
// the epoch claude's own rate-limit event states when it has one, and the prose
// clause in the refusal only when it has not (ADR-0233).
//
// The wire figure wins because it is the same fact without the two ways the
// sentence loses it — a wording pop's patterns do not cover, and a clock read in
// the wrong zone. Reading it is not Agent quota reporting: it is taken from the
// capture pop already consumes, only after the refusal, and never to ask how
// much allowance is left.
func claudeStreamQuotaResetAt(raw, reason string, now time.Time) time.Time {
	if at := claudeRateLimitResetAt(raw, now); !at.IsZero() {
		return at
	}
	return claudeQuotaResetAt(reason, now)
}

// claudeRateLimitResetAt reads the reset instant off the rate-limit events
// claude emits beside its refusal. Observed 2026-08-24, verbatim:
//
//	{"type":"rate_limit_event","rate_limit_info":{"status":"rejected",
//	 "resetsAt":1787598000,"rateLimitType":"five_hour",...}}
//
// Only a rejecting event dates a pause. The same event type also carries
// `allowed` and `allowed_warning` readings throughout a healthy run, and those
// describe a window pop is still spending in, not one it is waiting on. The last
// rejection wins, matching how the terminal result event is read.
//
// The stated epoch is padded by the Quota assurance offset, which is what it is
// for: this is a second-granular edge, and a retry that lands on it exactly is
// refused again — writing a fresh cooldown from the moment of that refusal
// rather than from the truth (ADR-0233).
//
// now bounds what may be believed. The prose clauses can only name an instant
// within the week, but an epoch can say anything, and the drain waits on this
// value directly — so a figure outside the horizon the cooldown store would
// accept is read as garbage rather than as a park lasting past it (ADR-0034).
func claudeRateLimitResetAt(raw string, now time.Time) time.Time {
	var latest int64
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type      string `json:"type"`
			RateLimit struct {
				Status   string `json:"status"`
				ResetsAt int64  `json:"resetsAt"`
			} `json:"rate_limit_info"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		if event.Type == "rate_limit_event" && event.RateLimit.Status == "rejected" && event.RateLimit.ResetsAt > 0 {
			latest = event.RateLimit.ResetsAt
		}
		return true
	})
	if latest == 0 {
		return time.Time{}
	}
	at := time.Unix(latest, 0).UTC().Add(quotaAssuranceOffset)
	if at.Sub(now.UTC()) > maxAgentQuotaResetHorizon {
		return time.Time{}
	}
	return at
}

// claudeQuotaResetAt reads the reset clause out of the refusal itself. It is the
// fallback for a capture that states no reset epoch of its own, so it stays as
// forgiving as the wording is: the hour may come without minutes, and the zone
// the message names decides which hour that is (ADR-0233).
func claudeQuotaResetAt(reason string, now time.Time) time.Time {
	return withQuotaAssuranceOffset(claudeQuotaResetClauseAt(reason, now))
}

// claudeQuotaResetClauseAt is the clause reading itself, unpadded — the instant
// the sentence names, which claudeQuotaResetAt then pads exactly once.
func claudeQuotaResetClauseAt(reason string, now time.Time) time.Time {
	loc := claudeResetZone(reason, now.Location())
	if m := claudeWeekdayResetAtPattern.FindStringSubmatch(reason); m != nil {
		hour, minute, ok := parseQuotaClock(m[2], m[3], m[4])
		if !ok {
			return time.Time{}
		}
		weekday, ok := parseQuotaWeekday(m[1])
		if !ok {
			return time.Time{}
		}
		return nextQuotaWeekdayTimeIn(now, loc, weekday, hour, minute)
	}
	if m := claudeBareResetAtPattern.FindStringSubmatch(reason); m != nil {
		hour, minute, ok := parseQuotaClock(m[1], m[2], m[3])
		if !ok {
			return time.Time{}
		}
		return nextQuotaTimeIn(now, loc, hour, minute)
	}
	return time.Time{}
}

// claudeResetZone resolves the zone a refusal names to a location, falling back
// to the caller's when the message names none or names one this machine has no
// zone database entry for. A zone pop cannot load is not worse than the zone it
// assumed before there was one to read.
func claudeResetZone(reason string, fallback *time.Location) *time.Location {
	m := claudeResetZonePattern.FindStringSubmatch(reason)
	if m == nil {
		return fallback
	}
	loc, err := time.LoadLocation(m[1])
	if err != nil {
		return fallback
	}
	return loc
}

// parseQuotaClock reads a 12-hour clock out of a refusal. An empty minuteText is
// the hour stated on its own ("9pm"), not a parse failure.
func parseQuotaClock(hourText, minuteText, meridiem string) (int, int, bool) {
	hour, err := strconv.Atoi(hourText)
	if err != nil || hour < 1 || hour > 12 {
		return 0, 0, false
	}
	minute := 0
	if minuteText != "" {
		minute, err = strconv.Atoi(minuteText)
		if err != nil || minute < 0 || minute > 59 {
			return 0, 0, false
		}
	}
	switch strings.ToUpper(meridiem) {
	case "AM":
		if hour == 12 {
			hour = 0
		}
	case "PM":
		if hour != 12 {
			hour += 12
		}
	default:
		return 0, 0, false
	}
	return hour, minute, true
}

func parseQuotaWeekday(text string) (time.Weekday, bool) {
	switch strings.ToLower(text) {
	case "sun":
		return time.Sunday, true
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}

func nextQuotaLocalTime(now time.Time, hour, minute int) time.Time {
	return nextQuotaTimeIn(now, now.Location(), hour, minute)
}

// nextQuotaTimeIn is nextQuotaLocalTime in a stated zone: the next occurrence of
// a wall clock, read where the provider meant it rather than where pop happens
// to be running (ADR-0233).
func nextQuotaTimeIn(now time.Time, loc *time.Location, hour, minute int) time.Time {
	localNow := now.In(loc)
	reset := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	if !reset.After(localNow) {
		reset = reset.Add(24 * time.Hour)
	}
	if reset.Sub(localNow) > 24*time.Hour {
		return time.Time{}
	}
	return reset
}

// nextQuotaWeekdayTimeIn is nextQuotaTimeIn for a clock that also names a day:
// the next occurrence of that weekday at that time, in the stated zone.
func nextQuotaWeekdayTimeIn(now time.Time, loc *time.Location, weekday time.Weekday, hour, minute int) time.Time {
	localNow := now.In(loc)
	reset := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	days := (int(weekday) - int(localNow.Weekday()) + 7) % 7
	reset = reset.AddDate(0, 0, days)
	if !reset.After(localNow) {
		reset = reset.AddDate(0, 0, 7)
	}
	return reset
}
