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
	return b.String()
}
