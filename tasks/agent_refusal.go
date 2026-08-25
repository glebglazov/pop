package tasks

import (
	"strings"
	"time"
)

// AgentQuotaWindowClass names which allowance a refusal exhausted — the window
// that has to reopen before the preset can run again. It is read from the typed
// field the refusal carries, and from the marker sentence that names it when
// there is no field to read (ADR-0234).
//
// The vocabulary is claude's, because claude is the adapter whose capture states
// it. An adapter whose provider names a window pop holds no class for reports
// Unknown rather than inventing one.
type AgentQuotaWindowClass string

const (
	// QuotaWindowUnknown is the stated absence: no channel of the capture named a
	// window. It is the zero value, so a refusal nobody classified never passes
	// itself off as one that was.
	QuotaWindowUnknown AgentQuotaWindowClass = ""
	// QuotaWindowFiveHour is the short rolling window claude spells `five_hour`
	// on the wire and announces in prose as a session limit.
	QuotaWindowFiveHour AgentQuotaWindowClass = "five_hour"
	// QuotaWindowWeekly is the account's week-long allowance.
	QuotaWindowWeekly AgentQuotaWindowClass = "weekly"
	// QuotaWindowOpus is the separate weekly allowance a provider keeps for its
	// most expensive model, which is exhausted while the ordinary weekly window
	// still has room.
	QuotaWindowOpus AgentQuotaWindowClass = "opus"
)

// Span is how long the window this class names runs, and so the longest a
// refusal against it can still be in force. A cooldown pop had to guess is
// dated from it: five hours for a session limit, a week for either weekly
// allowance (ADR-0235).
//
// Unknown reports false rather than a number. No channel of the capture named a
// window, so there is no span to date from and the caller falls back to its
// configured ceiling instead of inventing one here.
func (c AgentQuotaWindowClass) Span() (time.Duration, bool) {
	switch c {
	case QuotaWindowFiveHour:
		return 5 * time.Hour, true
	case QuotaWindowWeekly, QuotaWindowOpus:
		return 7 * 24 * time.Hour, true
	}
	return 0, false
}

// detectRefusal reports the quota refusal this signature finds in one Captured
// run: the structured channel first, the marker sentences beneath it (ADR-0234).
//
// raw is the whole capture, which is where typed fields live; result is the
// terminal transcript, which is where the provider's sentence lands. A Blind
// signature reads neither — the adapter's own prose detector answers for it —
// so it finds nothing here.
func (c AgentRefusalSignatureCapability) detectRefusal(raw, result string) *AgentProceedVerdict {
	class, structured := c.structuredRefusal(raw)
	markerClass, marked := c.markerRefusal(result)
	if !structured && !marked {
		return nil
	}
	// A capture that refused structurally but stated no window still has the
	// sentence beside it, and each sentence names its own limit. This is what the
	// demoted markers keep earning their line for.
	if class == QuotaWindowUnknown {
		class = markerClass
	}
	v := DetectedQuotaPause(refusalReason(result, class)).WithWindowClass(class)
	return &v
}

// structuredRefusal asks the typed channel, if this signature declares one.
func (c AgentRefusalSignatureCapability) structuredRefusal(raw string) (AgentQuotaWindowClass, bool) {
	if c.Kind != CapabilitySupported || c.Structured == nil {
		return QuotaWindowUnknown, false
	}
	return c.Structured(raw)
}

// markerRefusal matches the provider's own sentences against the transcript and
// answers the window class the matched one names.
func (c AgentRefusalSignatureCapability) markerRefusal(result string) (AgentQuotaWindowClass, bool) {
	for _, marker := range c.Markers {
		if marker.Sentence != "" && strings.Contains(result, marker.Sentence) {
			return marker.Class, true
		}
	}
	return QuotaWindowUnknown, false
}

// refusalReason is the sentence the verdict carries. The provider's own wording
// wins wherever there is any: the human reads it, and the reset clause is parsed
// back out of it. Only a capture that refused in its typed fields while saying
// nothing in prose — the very case structured detection exists for — gets pop's
// own sentence, rather than a blank quotation.
func refusalReason(result string, class AgentQuotaWindowClass) string {
	if strings.TrimSpace(result) != "" {
		return result
	}
	if class == QuotaWindowUnknown {
		return "the agent's capture reports a rate-limit rejection"
	}
	return "the agent's capture reports a rate-limit rejection on its " + string(class) + " window"
}
