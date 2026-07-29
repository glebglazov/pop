package tasks

import (
	"testing"
	"time"
)

func TestQuotaPauseUnavailabilityIsTimeHealing(t *testing.T) {
	resetAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	u := NewQuotaPauseUnavailability("claude", "weekly limit", resetAt)
	if u.Kind != UnavailabilityQuotaPause {
		t.Fatalf("kind = %q, want %q", u.Kind, UnavailabilityQuotaPause)
	}
	th, ok := u.TimeHealing()
	if !ok {
		t.Fatal("quota pause must be time-healing")
	}
	if !th.ResetAt.Equal(resetAt) {
		t.Fatalf("ResetAt = %v, want %v", th.ResetAt, resetAt)
	}
}

func TestHumanHealingCannotReportTimeHealing(t *testing.T) {
	u := NewAuthFailureUnavailability("cursor", "Authentication required")
	if u.Kind != UnavailabilityAuthFailure {
		t.Fatalf("kind = %q, want %q", u.Kind, UnavailabilityAuthFailure)
	}
	if _, ok := u.TimeHealing(); ok {
		t.Fatal("human-healing must not report TimeHealing — recovery wait accepts TimeHealingRecovery only")
	}
}

func TestMissingBinaryUnavailabilityIsHumanHealing(t *testing.T) {
	u := NewMissingBinaryUnavailability("cursor", "binary not found on PATH")
	if u.Kind != UnavailabilityMissingBinary {
		t.Fatalf("kind = %q, want %q", u.Kind, UnavailabilityMissingBinary)
	}
	if _, ok := u.TimeHealing(); ok {
		t.Fatal("missing binary must be human-healing")
	}
}
