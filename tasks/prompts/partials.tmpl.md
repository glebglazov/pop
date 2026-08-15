{{define "task-listing"}}{{range .}}- {{.ID}} [{{.Type}} {{.Status}}{{.EffortClause}}]{{.TitleClause}} ({{.Path}}){{.BlockedByClause}}
{{end}}{{end}}
{{define "task-body"}}{{if .Readable}}```markdown
{{.Body}}
```{{end}}{{if .Unreadable}}Could not read {{.Path}}: {{.Error}}.
Proceed by inspecting the task path manually or asking the human for the missing task body.{{end}}{{end}}
