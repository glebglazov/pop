package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ticketName = "ticket.md"

// ticketSeparator ends the ticket header. The header holds the status and the
// ticket's origin; only the body below it reaches the Ticket arm.
const ticketSeparator = "---"

// loadApprovedTicket returns the body of the Case's ticket, the request a
// Ticket arm implements. A human approves the ticket as they approve an
// Acceptance list, because an edit to it decides what the Arm is told.
func loadApprovedTicket(caseDir string) (string, error) {
	path := filepath.Join(caseDir, ticketName)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read Case ticket: %w", err)
	}
	header, body, found := strings.Cut(string(data), "\n"+ticketSeparator+"\n")
	if !found {
		return "", fmt.Errorf("Case ticket %s has no %q line between its header and its body", path, ticketSeparator)
	}
	approved := false
	for _, line := range strings.Split(header, "\n") {
		approved = approved || strings.EqualFold(strings.TrimSpace(line), "Status: approved")
	}
	if !approved {
		return "", fmt.Errorf("Case ticket is not approved; change the status line in %s to Status: approved", path)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("Case ticket %s has an empty body", path)
	}
	return body + "\n", nil
}

// ticketPrompt tells the agent that the request came before any planning and
// that no human can answer it, so a question in the ticket gets decided and
// built rather than answered in prose.
func ticketPrompt(manifest caseManifest, ticket string) string {
	return "Implement the request below in repository " + manifest.RepositoryURL + ".\n" +
		"The project owner wrote it before any planning. Where it asks a question or asks for your opinion, decide the answer yourself and implement it: no one can answer you during this run.\n" +
		"Complete all requested work and run the gate commands before you finish.\n" +
		"Do not make git commits.\n\nGate commands:\n" + strings.Join(manifest.GateCommands, "\n") +
		"\n\n## Request\n\n" + ticket
}

// checkTicketInputs refuses a Matrix before any Trial runs when a Ticket arm
// meets a Case with no approved ticket, so a missing ticket never shows as a
// Trial that was not run.
func checkTicketInputs(casesRoot string, cases, armNames []string, arms map[string]armFile) error {
	for _, name := range armNames {
		if arms[name].Input != armInputTicket {
			continue
		}
		for _, caseName := range cases {
			caseDir, err := resolveCaseDir(caseName, casesRoot)
			if err != nil {
				return err
			}
			if _, err := loadApprovedTicket(caseDir); err != nil {
				return fmt.Errorf("Arm %q needs an approved ticket for Case %s: %w", name, caseName, err)
			}
		}
	}
	return nil
}
