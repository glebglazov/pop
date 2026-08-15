package routine

import (
	"embed"
	"strings"

	"github.com/glebglazov/pop/internal/prompt"
)

// The Routine agent prompts live beside the code that owns them, as markdown a
// human can read and edit without touching Go (ADR-0208). Parsing at init means
// a malformed template fails the first test run rather than a live Fire.
//
//go:embed prompts/*.tmpl.md
var promptTemplateFS embed.FS

var promptTemplates = prompt.MustParseFS(promptTemplateFS, "prompts/*.tmpl.md")

// wrapPromptView is what the run wrapper's template renders against: the
// framework's own preamble and postamble around the routine's authored body.
type wrapPromptView struct {
	MemoryDir        string
	ReportPath       string
	DomainPrompt     string
	CompleteSentinel string
	FailedSentinel   string
}

func wrapRoutinePrompt(memoryDir, reportPath, domainPrompt string) string {
	return prompt.MustRender(promptTemplates, "wrap.tmpl.md", wrapPromptView{
		MemoryDir:        memoryDir,
		ReportPath:       reportPath,
		DomainPrompt:     strings.TrimRight(domainPrompt, "\n"),
		CompleteSentinel: routineCompleteSentinel,
		FailedSentinel:   routineFailedSentinel,
	})
}
