package cmd

import (
	"io"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// TestConfigDashboardRefusesWithoutATTY pins ADR-0202 decision 15: the TUI is
// this feature's only surface in this pass, so a redirected stdout is told so
// rather than handed a listing that is not the dashboard.
func TestConfigDashboardRefusesWithoutATTY(t *testing.T) {
	t.Parallel()
	err := runConfigDashboardWith(nil, ui.ConfigDashboardOpts{}, strings.NewReader(""), io.Discard, false)
	if err == nil {
		t.Fatal("no error with stdout redirected, want a refusal")
	}
	for _, want := range []string{"terminal", "not a TTY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
}

// TestConfigDashboardRowsCarryTheView checks the one adapter between the
// resolved config views and the component: the component decides layout, so
// every word it renders has to arrive through here.
func TestConfigDashboardRowsCarryTheView(t *testing.T) {
	t.Parallel()
	rows := configDashboardRows([]config.OverrideKeyView{{
		Key:           "work.verify.agents",
		Desc:          "Ordered fallback agent list for the Verifier.",
		Overridden:    true,
		Layer:         config.OverrideLayerOverride,
		EffectiveTOML: "work.verify.agents = []",
		Note:          "override set to an empty list — fallthrough disabled",
		SourceLayer:   config.OverrideLayerConfig,
		SourceTOML:    `work.verify.agents = ["claude"]`,
		Reach:         []config.ConfigKeyReachLine{{Actor: "claude", Detail: "--agents …"}},
	}})
	if len(rows) != 1 {
		t.Fatalf("%d rows for one view", len(rows))
	}
	row := rows[0]
	if row.Key != "work.verify.agents" || !row.Overridden {
		t.Errorf("row = %+v, want the key and its override marker", row)
	}
	if row.Preview.Provenance != "override" || row.Preview.SourceProvenance != "config.toml" {
		t.Errorf("provenance = %q / %q, want override over config.toml",
			row.Preview.Provenance, row.Preview.SourceProvenance)
	}
	if row.Preview.Note == "" || row.Preview.ValueTOML == "" || row.Preview.SourceTOML == "" {
		t.Errorf("preview lost text: %+v", row.Preview)
	}
	if len(row.Preview.Reach) != 1 || row.Preview.Reach[0].Actor != "claude" {
		t.Errorf("reach = %+v, want the declared line", row.Preview.Reach)
	}
}

// TestConfigDashboardAgentKeysDeclareNoReach pins the deferral ADR-0202 records:
// reach for the four agent lists is a separate question, so their previews carry
// no reach block in this pass.
func TestConfigDashboardAgentKeysDeclareNoReach(t *testing.T) {
	t.Parallel()
	keys := config.OverrideKeys()
	if len(keys) == 0 {
		t.Fatal("override registry is empty; nothing to render")
	}
	views := make([]config.OverrideKeyView, 0, len(keys))
	for _, key := range keys {
		if reach, ok := config.ConfigKeyReachFor(key.Key); ok {
			t.Errorf("%s declares reach %+v; this test's premise is stale", key.Key, reach)
		}
		views = append(views, config.OverrideKeyView{Key: key.Key, Desc: key.Desc})
	}
	for _, row := range configDashboardRows(views) {
		if row.Preview.Reach != nil {
			t.Errorf("row %s carries a reach block: %+v", row.Key, row.Preview.Reach)
		}
	}
}

// TestConfigDashboardHelpDocumentsThePopupBinding keeps the larger binding
// ADR-0202 decision 13 promises beside the command a human runs.
func TestConfigDashboardHelpDocumentsThePopupBinding(t *testing.T) {
	t.Parallel()
	if !strings.Contains(configDashboardCmd.Long, "display-popup") {
		t.Errorf("config dashboard help documents no tmux binding:\n%s", configDashboardCmd.Long)
	}
}
