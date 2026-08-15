// Package prompt is the render seam every agent Prompt template passes through:
// parse once at package init, execute against a Prompt view, normalize what
// comes out.
//
// It holds no prompt text. Templates and the contracts they render stay in the
// domain package that owns them (ADR-0208); what is shared here is the
// whitespace bookkeeping and the failure policy, which are properties of the
// renderer rather than of any one prompt.
package prompt

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"text/template"
)

// MustParseFS parses a package's embedded templates, panicking on a malformed
// one. Callers assign the result to a package-level var, so the parse happens at
// init: an unparseable template is a build-time mistake, and panicking there
// matches how this repo treats every other compiled-once asset.
func MustParseFS(fsys fs.FS, patterns ...string) *template.Template {
	return template.Must(template.New("prompts").ParseFS(fsys, patterns...))
}

// MustRender executes the named template against view and returns the
// normalized prompt.
//
// It panics on an execute failure rather than returning what it managed to
// render. The text is a briefing for an agent that then edits a checkout, so a
// silently truncated prompt does damage a crash does not (ADR-0208).
func MustRender(t *template.Template, name string, view any) string {
	var b bytes.Buffer
	if err := t.ExecuteTemplate(&b, name, view); err != nil {
		panic(fmt.Sprintf("render prompt template %q: %v", name, err))
	}
	return Normalize(b.String())
}

// Normalize is the whitespace pass that frees the templates from hand-tuning
// `{{- -}}` markers around every conditional section. A naked `{{if}}` line
// leaves the blank line it sat on behind, so an absent section shows up as a
// widened gap; the pass closes it.
//
// Concretely: every line loses its trailing spaces and tabs, any run of blank
// lines collapses to one, and the result ends in exactly one newline. Text that
// is entirely blank normalizes to the empty string.
func Normalize(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	pendingBlank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			pendingBlank = true
			continue
		}
		if pendingBlank && len(out) > 0 {
			out = append(out, "")
		}
		pendingBlank = false
		out = append(out, line)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}
