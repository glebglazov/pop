package workload

func normalizeCursorStreamJSON(raw string) AgentResult {
	return normalizeResultStreamJSON(raw, nil)
}
