package tasks

import (
	"strings"

	"github.com/glebglazov/pop/config"
)

// implementRefineConvention resolves the `refine` convention block every
// implement prompt of this run carries, or empty when it carries none
// (ADR-0240). The toggle it reads, [work.implement].include_refine_convention,
// is independent of [work.refine].enabled: a repository may hold its builders
// to the standard upfront long before it switches the pass on.
//
// It is resolved once per run rather than per task because the answer describes
// the repository being drained, not any one task — and every task of the run,
// planned and Remediation alike, is held to the same text. An unwired seam or a
// resolution failure both mean the same thing here: the prompt carries no
// convention, and the run goes on. Nothing about a builder's instructions is
// worth failing a drain over.
func implementRefineConvention(cfg *config.Config, resolve RefineConvention, cwd string) string {
	if !cfg.ImplementIncludesRefineConvention() || resolve == nil {
		return ""
	}
	prose, err := resolve(cwd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(prose)
}
