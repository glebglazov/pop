package config

import (
	"testing"
)

// TestConfigKeyReachAdmitsANonAgentKey pins ADR-0198's registration seam: a
// second key whose actors have nothing to do with agent presets registers the
// same way turn_cap will, from above rather than by config importing down.
func TestConfigKeyReachAdmitsANonAgentKey(t *testing.T) {
	t.Cleanup(func() { ClearConfigKeyReach("disk_budget") })

	if _, ok := ConfigKeyReachFor("disk_budget"); ok {
		t.Fatal("disk_budget should start with no declared reach")
	}

	RegisterConfigKeyReach("disk_budget", ConfigKeyReach{
		Lines: []ConfigKeyReachLine{
			{Actor: "local-ssd", Detail: "--quota 4G"},
			{Actor: "network-volume", Detail: "network volumes ignore disk_budget; size is the share's own cap"},
		},
	})

	got, ok := ConfigKeyReachFor("disk_budget")
	if !ok {
		t.Fatal("disk_budget reach was not registered")
	}
	if len(got.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(got.Lines))
	}
	if got.Lines[0].Actor != "local-ssd" || got.Lines[0].Detail != "--quota 4G" {
		t.Errorf("first line = %+v, want local-ssd / --quota 4G", got.Lines[0])
	}
	if got.Lines[1].Actor != "network-volume" {
		t.Errorf("second actor = %q, want network-volume", got.Lines[1].Actor)
	}

	ClearConfigKeyReach("disk_budget")
	if _, ok := ConfigKeyReachFor("disk_budget"); ok {
		t.Fatal("ClearConfigKeyReach left disk_budget declared")
	}
}
