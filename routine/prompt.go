package routine

import (
	"fmt"
	"strings"
)

func wrapRoutinePrompt(memoryDir, reportPath, domainPrompt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Before starting, read the routine memory directory at %s and incorporate any prior context.\n\n", memoryDir)
	b.WriteString(strings.TrimRight(domainPrompt, "\n"))
	fmt.Fprintf(&b, "\n\nWhen finished, write your report to %s and update the routine memory directory at %s with what you learned.\n", reportPath, memoryDir)
	fmt.Fprintf(&b, "\nEnd your output with a completion sentinel on its own line, exactly one of:\n")
	fmt.Fprintf(&b, "  %s   (the run completed and the report was written)\n", routineCompleteSentinel)
	fmt.Fprintf(&b, "  %s: <reason>   (the run could not be completed)\n", routineFailedSentinel)
	fmt.Fprintf(&b, "Without %s the run is recorded failed even if you exit cleanly.\n", routineCompleteSentinel)
	return b.String()
}
