package routine

import (
	"embed"
	"path/filepath"
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

// The two things the wrapper needs that do not exist yet while a routine is
// being authored: no body is written, and no run has fired to stamp a report.
const (
	placeholderRoutineBody = "<the body of your prompt file, verbatim>"
	placeholderReportName  = "<timestamp>.md"
)

// frameworkContractExample is what the authoring prompts show instead of
// describing the wrapper in prose: the real wrapper's output, called with
// placeholders. An edit to wrap.tmpl.md reaches both authoring prompts on the
// next render, so the contract they teach cannot drift from the one a run gets.
func frameworkContractExample(memoryDir, runsDir string) string {
	wrapped := wrapRoutinePrompt(memoryDir, filepath.Join(runsDir, placeholderReportName), placeholderRoutineBody)
	// The render carries its own trailing newline; the template that embeds it
	// supplies the one before the closing fence.
	return strings.TrimRight(wrapped, "\n")
}
