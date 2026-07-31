package tasks

// extractToolTimings applies the agent's declared tool-timing capability to a
// Captured run's stored events (ADR 0016, ADR-0165). Blind or unknown
// adapters return no per-tool rows and no tool windows.
func extractToolTimings(agent string, events []streamEventRecord) ([]ToolTiming, []toolWindow) {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return nil, nil
	}
	cap := adapter.ToolTimingCapability()
	if cap.Kind != CapabilitySupported || cap.Extract == nil {
		return nil, nil
	}
	return cap.Extract(events)
}

// extractActualModel applies the agent's declared actual-model capability.
// Blind or unknown adapters return "".
func extractActualModel(agent string, events []streamEventRecord) string {
	adapter, ok := agentAdapters[agent]
	if !ok {
		return ""
	}
	cap := adapter.ActualModelCapability()
	if cap.Kind != CapabilitySupported || cap.Extract == nil {
		return ""
	}
	return cap.Extract(events)
}
