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
	answerHeader          = regexp.MustCompile(`(?i)^##\s+Answer\s*$`)
)

// answerSectionName is the one section of a Decision ticket pop writes;
// answerRegionName names the generated region that delimits pop's part of it.
const (
	answerSectionName = "Answer"
	answerRegionName  = "answer"
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

// parseMapStatus reads the declared vocabulary and nothing else. `done` is a hard
// cut with no fold: the only Maps that exist are per-machine and none of them is
// `done`, so an alias would keep a dead word in the parser forever. The error is
// the corrective a reader of a BROKEN Map acts on, so it names the replacement
// rather than just rejecting the word.
func parseMapStatus(raw string) (MapStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(MapActive):
		return MapActive, nil
	case string(MapArrived):
		return MapArrived, nil
	case string(MapAbandoned):
		return MapAbandoned, nil
	default:
		return "", fmt.Errorf("unknown map status %q: map.md's Status: line takes active | arrived | abandoned "+
			"(a Map that reached its destination is `arrived` — write it with `pop map arrive <map-id>`)", raw)
	}
}

// ReplaceMapStatus writes status onto map.md's Status: line. Only the metadata
// region above the first `## ` heading is considered — further down, `Status:` is
// prose a session wrote. A Map charted without the line reads as active, so one is
// inserted where every existing map keeps it: under the title, above the sections.
func ReplaceMapStatus(content string, status MapStatus) string {
	line := "Status: " + string(status)
	lines := strings.Split(content, "\n")
	insertAt := 0
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if statusLinePattern.MatchString(trimmed) {
			lines[i] = line
			return strings.Join(lines, "\n")
		}
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if strings.HasPrefix(trimmed, "#") {
			insertAt = i + 1
		}
	}
	out := append([]string{}, lines[:insertAt]...)
	if insertAt > 0 {
		out = append(out, "")
	}
	out = append(out, line, "")
	rest := lines[insertAt:]
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	return strings.Join(append(out, rest...), "\n")
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

// sectionBounds locates the body of a `## ` section: the half-open line range
// between its heading and the next heading of the same level.
func sectionBounds(lines []string, header *regexp.Regexp) (start, end int, found bool) {
	start = -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			if header.MatchString(trimmed) {
				start = i + 1
			}
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			return start, i, true
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	return start, len(lines), true
}

// ParseTicketAnswer returns the body of a Decision ticket's `## Answer` section,
// with the surrounding blank lines removed. Empty when the ticket carries no
// answer — which is how an unresolved ticket reads.
//
// The answer pop wrote sits between generated-region markers; a ticket written
// before the markers existed is read the way it will be folded — heading to end
// of file — so that what is read back is what the next resolve replaces.
func ParseTicketAnswer(content string) string {
	lines := strings.Split(content, "\n")
	if body, marked := generatedRegionBody(lines, answerRegionName); marked {
		return strings.Trim(body, "\n \t")
	}
	start, _, found := sectionBounds(lines, answerHeader)
	if !found {
		return ""
	}
	return strings.Trim(strings.Join(lines[start:], "\n"), "\n \t")
}

// ReplaceTicketAnswer writes body as the ticket's `## Answer`, wrapped in the
// generated-region markers that mark it as pop's to overwrite. Replacement rather
// than appending is what makes the resolve verb re-runnable: a wrong answer is
// corrected by resolving again, and a ticket never accumulates a stack of answers
// a reader has to date-order.
//
// The markers, not the heading structure, delimit the region, so an answer body
// carrying its own `## ` headings is still replaced whole. An `## Answer` that
// predates them is folded on this write: a Decision ticket is `## Question` then
// `## Answer`, so the answer is terminal and everything below the heading is
// pop's — including the duplicate body a pre-marker resolve may have left there.
func ReplaceTicketAnswer(content, body string) string {
	var bodyLines []string
	if trimmed := strings.Trim(body, "\n"); trimmed != "" {
		bodyLines = strings.Split(trimmed, "\n")
	}
	block := generatedRegionBlock(answerRegionName, bodyLines)

	lines := strings.Split(content, "\n")
	if openIdx, closeIdx, marked := locateGeneratedRegion(lines, answerRegionName); marked {
		out := append([]string{}, lines[:openIdx]...)
		out = append(out, block...)
		out = append(out, lines[closeIdx+1:]...)
		return strings.Join(trimTrailingBlank(out), "\n") + "\n"
	}

	start, _, found := sectionBounds(lines, answerHeader)
	if !found {
		out := append([]string{}, trimTrailingBlank(lines)...)
		out = append(out, "", "## "+answerSectionName, "")
		return strings.Join(append(out, block...), "\n") + "\n"
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, "")
	out = append(out, block...)
	return strings.Join(trimTrailingBlank(out), "\n") + "\n"
}

// AnswerGist condenses an answer body into the single line that stands for the
// decision in map.md's index: its first line of prose, whitespace collapsed.
func AnswerGist(answer string, maxLen int) string {
	for _, line := range strings.Split(answer, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return DestinationGist(trimmed, maxLen)
	}
	return ""
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
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
