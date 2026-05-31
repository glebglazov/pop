package workload

import (
	"fmt"
	"path/filepath"
	"strings"
)

// BuildAgentPrompt generates the instruction prompt for an issue attempt.
func BuildAgentPrompt(issuePath, runtimePath string) string {
	issuesDir := filepath.Dir(issuePath)
	var b strings.Builder
	fmt.Fprintf(&b, "You are implementing the issue at: %s\n\n", issuePath)
	fmt.Fprintf(&b, "Read the issue file in full. Follow any optional context references it\n")
	fmt.Fprintf(&b, "contains (for example a \"## Parent\" section) when present; the issue may also\n")
	fmt.Fprintf(&b, "be self-contained. Implement the work described under \"What to build\" and\n")
	fmt.Fprintf(&b, "satisfy every box under \"Acceptance criteria\". As you complete each\n")
	fmt.Fprintf(&b, "criterion, check its box (`- [ ]` → `- [x]`) in %s.\n\n", issuePath)
	fmt.Fprintf(&b, "Do NOT modify %s. Do NOT modify other issue files in %s.\n",
		filepath.Join(issuesDir, "index.json"), issuesDir)
	fmt.Fprintf(&b, "Do NOT make git commits — the runner handles assessment and committing.\n\n")
	fmt.Fprintf(&b, "Runtime checkout: %s\n\n", runtimePath)
	fmt.Fprintf(&b, "Implementation edits belong only beneath the runtime checkout.\n\n")
	fmt.Fprintf(&b, "When you have completed the work, print a summary block followed by the\n")
	fmt.Fprintf(&b, "completion sentinel as the final lines of your output, exactly:\n\n")
	fmt.Fprintf(&b, "SUMMARY_START\n")
	fmt.Fprintf(&b, "<one or more lines describing what you did>\n")
	fmt.Fprintf(&b, "SUMMARY_END\n")
	fmt.Fprintf(&b, "TASK_COMPLETE\n\n")
	fmt.Fprintf(&b, "If you cannot complete the task (blocked, unclear, missing info, repeated\n")
	fmt.Fprintf(&b, "failure), instead print as the final line:\n\n")
	fmt.Fprintf(&b, "TASK_FAILED: <one-line reason>\n")
	return b.String()
}
