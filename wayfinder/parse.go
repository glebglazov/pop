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
		Status: TicketOpen,
	}
	if dash := strings.Index(base, "-"); dash > 0 {
		ticket.Slug = strings.TrimSuffix(base[dash+1:], ".md")
	}

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := typeLinePattern.FindStringSubmatch(trimmed); m != nil {
			parsed, err := parseTicketType(strings.TrimSpace(m[1]))
			if err != nil {
				return Ticket{}, err
			}
			ticket.Type = parsed
			continue
		}
		if m := statusLinePattern.FindStringSubmatch(trimmed); m != nil {
			parsed, err := parseTicketStatus(strings.TrimSpace(m[1]))
			if err != nil {
				return Ticket{}, err
			}
			ticket.Status = parsed
			continue
		}
		if m := blockedByPattern.FindStringSubmatch(trimmed); m != nil {
			ticket.BlockedBy = parseBlockedBy(m[1])
			continue
		}
		if !strings.Contains(trimmed, ":") {
			break
		}
	}
	return ticket, nil
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
