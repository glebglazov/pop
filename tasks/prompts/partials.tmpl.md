{{define "task-listing"}}{{range .}}- {{.ID}} [{{.Type}} {{.Status}}{{.EffortClause}}]{{.TitleClause}} ({{.Path}}){{.BlockedByClause}}
{{end}}{{end}}
{{define "task-body"}}{{if .Readable}}```markdown
{{.Body}}
```{{end}}{{if .Unreadable}}Could not read {{.Path}}: {{.Error}}.
Proceed by inspecting the task path manually or asking the human for the missing task body.{{end}}{{end}}
{{define "disposition-invariant"}}The human owns the transition. You do not effect a disposition — no task status change (complete, skip, reset, reopen), no verdict recorded, no accept, no remediation spawned — even when the human has told you which outcome they want; they effect it themselves after you exit.
You may draft what the human then confirms. A task body, a Remediation task, an edit to the task manifest, or implementation under the runtime checkout are all yours to prepare when the human asks for them: preparing an artifact is not deciding the outcome. Say plainly what you prepared, and leave the transition to the human.{{end}}
