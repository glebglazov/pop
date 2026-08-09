package tasks

import (
	"strconv"
	"strings"

	"github.com/glebglazov/pop/config"
)

func init() {
	config.RegisterConfigKeyReach("turn_cap", turnCapReach())
}

// turnCapReach flattens every built-in preset's turn-cap enforcement stance into
// the reach turn_cap declares (ADR-0198). Supported presets contribute the argv
// shape with the bound left as N; Blind presets contribute the Reason already
// carried on the capability — those sentences are not restated here.
func turnCapReach() config.ConfigKeyReach {
	lines := make([]config.ConfigKeyReachLine, 0, len(agentCatalogOrder))
	for _, preset := range agentCatalogOrder {
		adapter := agentAdapters[preset]
		lines = append(lines, config.ConfigKeyReachLine{
			Actor:  preset,
			Detail: turnCapReachDetail(adapter),
		})
	}
	return config.ConfigKeyReach{Lines: lines}
}

// turnCapReachSample is the bound turnCapReachDetail asks a Supported adapter to
// spell, so the reach can show the flag shape without a run's cap in hand. It is
// swapped back out for "N" in the rendered detail.
const turnCapReachSample = 7

func turnCapReachDetail(adapter AgentAdapter) string {
	cap := adapter.TurnCapEnforcementCapability()
	if cap.Kind != CapabilitySupported || cap.SpecTokens == nil {
		return cap.Reason
	}
	shape := strings.Join(cap.SpecTokens(turnCapReachSample), " ")
	return strings.ReplaceAll(shape, strconv.Itoa(turnCapReachSample), "N")
}
