package wayfinder

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	ticketFilePattern = regexp.MustCompile(`^(\d+)-.+\.md$`)
	statusLinePattern = regexp.MustCompile(`(?i)^Status:\s*(.+)$`)
	typeLinePattern   = regexp.MustCompile(`(?i)^Type:\s*(.+)$`)
	blockedByPattern  = regexp.MustCompile(`(?i)^Blocked by:\s*(.+)$`)
	destinationHeader     = regexp.MustCompile(`(?i)^##\s+Destination\s*$`)
	decisionsSoFarHeader  = regexp.MustCompile(`(?i)^##\s+Decisions so far\s*$`)
)

// ParseMapMarkdown extracts map status and destination from map.md contents.
func ParseMapMarkdown(content string) (MapStatus, string, error) {
	lines := strings.Split(content, "\n")
	var status MapStatus
	statusSet := false
	destStart := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := statusLinePattern.FindStringSubmatch(trimmed); m != nil {
			parsed, err := parseMapStatus(strings.TrimSpace(m[1]))
			if err != nil {
				return "", "", err
			}
			status = parsed
			statusSet = true
			continue
		}
		if destinationHeader.MatchString(trimmed) {
			destStart = i + 1
			break
		}
	}
	if !statusSet {
		status = MapActive
	}
	destination := extractDestination(lines, destStart)
	return status, destination, nil
}

func parseMapStatus(raw string) (MapStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return MapActive, nil
	case "done":
		return MapDone, nil
	case "abandoned":
		return MapAbandoned, nil
	default:
		return "", fmt.Errorf("unknown map status %q", raw)
	}
}

func extractDestination(lines []string, start int) string {
	if start < 0 || start >= len(lines) {
		return ""
	}
	var body []string
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if trimmed != "" {
			body = append(body, trimmed)
		}
	}
	return strings.Join(body, " ")
}

// ParseTicketMarkdown extracts ticket metadata from an issues/*.md file.
func ParseTicketMarkdown(filename, content string) (Ticket, error) {
	base := filepathBase(filename)
	m := ticketFilePattern.FindStringSubmatch(base)
	if m == nil {
		return Ticket{}, fmt.Errorf("ticket filename %q does not match NN-<slug>.md", filename)
	}
	number, err := strconv.Atoi(m[1])
	if err != nil {
		return Ticket{}, fmt.Errorf("ticket number in %q: %w", filename, err)
	}

	ticket := Ticket{
		Number: number,
		ID:     normalizeTicketID(m[1]),
		File:   base,
		Status: TicketOpen,
	}
	if dash := strings.Index(base, "-"); dash > 0 {
		ticket.Slug = strings.TrimSuffix(base[dash+1:], ".md")
	}

	_, headers := ticketHeaderLines(content)
	for _, h := range headers {
		switch h.kind {
		case headerType:
			parsed, err := parseTicketType(h.value)
			if err != nil {
				return Ticket{}, err
			}
			ticket.Type = parsed
		case headerStatus:
			parsed, err := parseTicketStatus(h.value)
			if err != nil {
				return Ticket{}, err
			}
			ticket.Status = parsed
		case headerBlockedBy:
			ticket.BlockedBy = parseBlockedBy(h.value)
		}
	}
	return ticket, nil
}

// The three metadata facts a pre-manifest ticket markdown carried in its header.
const (
	headerType      = "type"
	headerStatus    = "status"
	headerBlockedBy = "blocked-by"
)

// ticketHeaderLine is one matched metadata line, keyed by which fact it carries
// and pinned to the line it occupied.
type ticketHeaderLine struct {
	index int
	kind  string
	value string
}

// ticketHeaderLines splits a ticket markdown and walks its metadata region: from
// the top, past blanks and headings, to the first line that is not `key: value`.
// One walk feeds both readers — the parser that interprets the facts and the fold
// that deletes the lines — so stripping can never remove a line the parser did
// not read, nor leave one it did.
func ticketHeaderLines(content string) ([]string, []ticketHeaderLine) {
	lines := strings.Split(content, "\n")
	var headers []ticketHeaderLine
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		switch {
		case typeLinePattern.MatchString(trimmed):
			m := typeLinePattern.FindStringSubmatch(trimmed)
			headers = append(headers, ticketHeaderLine{index: i, kind: headerType, value: strings.TrimSpace(m[1])})
		case statusLinePattern.MatchString(trimmed):
			m := statusLinePattern.FindStringSubmatch(trimmed)
			headers = append(headers, ticketHeaderLine{index: i, kind: headerStatus, value: strings.TrimSpace(m[1])})
		case blockedByPattern.MatchString(trimmed):
			m := blockedByPattern.FindStringSubmatch(trimmed)
			headers = append(headers, ticketHeaderLine{index: i, kind: headerBlockedBy, value: m[1]})
		case !strings.Contains(trimmed, ":"):
			return lines, headers
		}
	}
	return lines, headers
}

// StripTicketHeaders removes the Status: / Type: / Blocked by: lines a ticket
// markdown carried before Maps had a manifest, leaving the body untouched. The
// blank lines the removal orphans in the metadata region are collapsed; blanks in
// the body are load-bearing and left alone.
func StripTicketHeaders(content string) string {
	lines, headers := ticketHeaderLines(content)
	if len(headers) == 0 {
		return content
	}
	drop := make(map[int]bool, len(headers))
	last := 0
	for _, h := range headers {
		drop[h.index] = true
		if h.index > last {
			last = h.index
		}
	}

	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if drop[i] {
			continue
		}
		if i <= last+1 && strings.TrimSpace(line) == "" {
			if len(out) == 0 || strings.TrimSpace(out[len(out)-1]) == "" {
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func filepathBase(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

func normalizeTicketID(raw string) string {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return raw
	}
	if n < 10 && !strings.HasPrefix(raw, "0") {
		return fmt.Sprintf("%02d", n)
	}
	return raw
}

func parseTicketType(raw string) (TicketType, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "research":
		return TicketResearch, nil
	case "prototype":
		return TicketPrototype, nil
	case "grilling":
		return TicketGrilling, nil
	case "task":
		return TicketTask, nil
	default:
		return "", fmt.Errorf("unknown ticket type %q", raw)
	}
}

func parseTicketStatus(raw string) (TicketStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "open":
		return TicketOpen, nil
	case "claimed":
		return TicketClaimed, nil
	case "resolved":
		return TicketResolved, nil
	default:
		return "", fmt.Errorf("unknown ticket status %q", raw)
	}
}

func parseBlockedBy(raw string) []string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, normalizeTicketID(part))
	}
	return out
}

// ParseDecisionsSoFar extracts the Decisions so far section from map.md contents.
func ParseDecisionsSoFar(content string) string {
	lines := strings.Split(content, "\n")
	start := -1
	for i, line := range lines {
		if decisionsSoFarHeader.MatchString(strings.TrimSpace(line)) {
			start = i + 1
			break
		}
	}
	return extractSectionBody(lines, start)
}

func extractSectionBody(lines []string, start int) string {
	if start < 0 || start >= len(lines) {
		return ""
	}
	var body []string
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		body = append(body, trimmed)
	}
	return strings.Join(body, " ")
}

// DestinationGist returns a short single-line summary of a destination.
func DestinationGist(destination string, maxLen int) string {
	oneLine := strings.Join(strings.Fields(destination), " ")
	if maxLen <= 0 || len(oneLine) <= maxLen {
		return oneLine
	}
	if maxLen <= 3 {
		return oneLine[:maxLen]
	}
	return oneLine[:maxLen-3] + "..."
}
