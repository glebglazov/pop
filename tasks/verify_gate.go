package tasks

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// verifyGateSession binds the current set to the Verify inputs for this run.
// The choice stays on the Attended session when a refreshed gate replaces
// this binding.
type verifyGateSession struct {
	reverify *reverifyGateContext
	manifest *Manifest
}

func (e *gateEnv) bindVerify(m *Manifest, rv *reverifyGateContext) {
	if e.cfg == nil {
		e.cfg = NewAttendedSession(nil, "")
	}
	if rv == nil {
		rv = e.reverify
	}
	if rv == nil {
		rv = &reverifyGateContext{cfg: e.cfg.Value()}
	}
	if e.cfg.refine != nil {
		e.cfg.refine.manifest = m
	}
	e.reverify = rv
	e.cfg.verify = &verifyGateSession{reverify: rv, manifest: m}
}

func (v *verifyGateSession) entries() []AgentGroupEntry {
	entries := v.reverify.cfg.VerifyAgentEntries()
	if over := v.manifest.VerifierOverride(); over != nil && len(nonEmptyStrings(over.Agents)) > 0 {
		entries = nil
		for _, spec := range nonEmptyStrings(over.Agents) {
			entries = append(entries, config.AgentEntry{Cmd: spec})
		}
	} else if len(entries.Commands()) == 0 {
		sel, err := resolveVerifier(nil, "", v.manifest, v.reverify.cfg)
		if err != nil {
			return nil
		}
		entries = nil
		for _, spec := range sel.Agents {
			entry := config.AgentEntry{Cmd: spec}
			for _, inherited := range v.reverify.cfg.ImplementAgentEntries() {
				if inherited.Cmd == spec {
					entry = inherited
					break
				}
			}
			entries = append(entries, entry)
		}
	}
	cfg := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Agents: entries}}}
	return usableGroupEntries(cfg, "verify")
}

func (v *verifyGateSession) decorate(spec *ui.GateMenuSpec, choice *AgentGroupEntry) {
	sel, err := resolveVerifier(v.reverify.agents, v.reverify.effort, v.manifest, v.reverify.cfg)
	label := ""
	var entry AgentGroupEntry
	if choice != nil {
		entry = *choice
	} else if err == nil && len(sel.Agents) > 0 {
		entry = LaunchedAttendedEntry(nil, sel.Agents[0])
		for _, configured := range v.entries() {
			if configured.Cmd == entry.Cmd {
				entry = configured
				break
			}
		}
	}
	if entry.Cmd != "" {
		// The fallback walk uses this same effort resolution at launch.
		resolved := resolveEffortModel(entry.Cmd, sel.Effort, true, v.reverify.cfg, nil)
		entry.Model = AgentSpecModel(resolved.Spec)
		label = FormatAgentEntry(entry)
	}
	for i := range spec.Items {
		if spec.Items[i].Role == "verify" {
			spec.Items[i].AgentLabel = label
			spec.Items[i].AgentPickable = len(v.entries()) > 1
		}
	}
}

func (e gateEnv) manualVerify(m *Manifest) (*Manifest, error) {
	rv := *e.cfg.verify.reverify
	rv.choice = e.cfg.verifyChoice
	rv.admission = AdmissionWait
	id, err := ResolveRepositoryIdentity(e.d, e.runtimePath)
	if err != nil {
		return nil, err
	}
	runErr := reverifyAtGate(e.d, &rv, e.out, id.CommonDir, e.runtimePath, e.taskSetID, m)
	after, err := RefreshWith(e.d, e.definitionPath, e.statePath)
	if err != nil {
		return nil, err
	}
	ApplyVerifyVerdicts(e.d, after, rv.cfg, e.runtimePath)
	fmt.Fprintln(e.out)
	Render(e.out, after)
	return after.Manifests[e.taskSetID], runErr
}

func (e gateEnv) verifyAndReturnToMenu(m *Manifest) (bool, error) {
	fresh, err := e.manualVerify(m)
	if err != nil {
		fmt.Fprintf(outputFor(e.out), "Could not re-verify: %v\n", err)
	}
	if fresh == nil {
		fresh = m
	}
	return e.reopenGateMenu(fresh)
}

func (e gateEnv) reopenGateMenu(m *Manifest) (bool, error) {
	id, err := ResolveRepositoryIdentity(e.d, e.runtimePath)
	if err != nil {
		return false, err
	}
	status, findings, sha := assistDerivedStatus(e.d, e.cfg.Value(), m, e.taskSetID, e.runtimePath, id.CommonDir)
	switch {
	case status == StatusVerifyFailed:
		return handleInteractiveVerifyFailedGate(e, id.CommonDir, m, sha, findings)
	case BlockingHITLTask(m) != nil:
		return handleInteractiveHITLGate(e, m, BlockingHITLTask(m), e.reverify)
	case FailedTask(m) != nil:
		return handleInteractiveFailedGate(e, m, FailedTask(m))
	default:
		return handleGenericAssistMenu(e, m, status, findings)
	}
}

func (e gateEnv) readVerifyReport(m *Manifest) {
	if pointer, ok := latestVerifyPointer(e.d, m); ok {
		pageReportDocument(e.d, e.in, e.runtimePath, e.out, pointer)
	}
}

func appendVerifyReportItem(items []ui.GateMenuItem, d *Deps, m *Manifest) []ui.GateMenuItem {
	if pointer, ok := latestVerifyPointer(d, m); ok {
		return append(items, ui.GateMenuItem{Key: "r", Label: "Read the verify report (no agent runs)", Details: []string{pointer.Path}})
	}
	return items
}
