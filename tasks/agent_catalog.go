package tasks

import (
	"fmt"
	"os/exec"
)

const DefaultAgentPreset = "claude"

var agentCatalogOrder = []string{"claude", "opencode", "cursor", "codex", "pi"}

// AgentCatalogRow describes Pop's recognition and PATH availability for one
// built-in agent preset.
type AgentCatalogRow struct {
	Agent  string
	Binary string
	Found  bool
	Notes  string
}

// AgentCatalog returns stable rows for every recognized built-in agent preset.
// It performs PATH lookup only; it does not invoke agent binaries.
func AgentCatalog(d *Deps) []AgentCatalogRow {
	lookPath := exec.LookPath
	if d != nil && d.LookPath != nil {
		lookPath = d.LookPath
	}

	rows := make([]AgentCatalogRow, 0, len(agentCatalogOrder))
	for _, preset := range agentCatalogOrder {
		adapter := agentAdapters[preset]
		binary := agentBinary(adapter)
		_, err := lookPath(binary)
		rows = append(rows, AgentCatalogRow{
			Agent:  preset,
			Binary: binary,
			Found:  err == nil,
			Notes:  agentCatalogNotes(preset, adapter),
		})
	}
	return rows
}

func agentBinary(adapter AgentAdapter) string {
	if preset, ok := adapter.(*presetAgentAdapter); ok && len(preset.headlessPrefix) > 0 {
		return preset.headlessPrefix[0]
	}
	return adapter.Preset()
}

func agentCatalogNotes(preset string, adapter AgentAdapter) string {
	capability := adapter.AssistanceCapability()
	if capability.Mode == AgentAssistanceFallback && capability.Command != nil && capability.Command.Name != "" {
		return fmt.Sprintf("HITL assistance falls back to %s", capability.Command.Name)
	}
	if preset == DefaultAgentPreset {
		return "default; accepts extra args, e.g. --model <alias>"
	}
	return "accepts extra args"
}
