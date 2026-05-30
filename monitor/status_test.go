package monitor

import "testing"

func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		raw  string
		want PaneStatus
	}{
		{"clear", StatusClear},
		{"idle", StatusClear},
		{"read", StatusClear},
		{"working", StatusWorking},
		{"unread", StatusUnread},
		{"needs_attention", StatusUnread},
		{"unknown", StatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := NormalizeStatus(tt.raw); got != tt.want {
				t.Errorf("NormalizeStatus(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIsClear(t *testing.T) {
	for _, s := range []PaneStatus{StatusClear, legacyStatusIdle, legacyStatusRead} {
		if !IsClear(s) {
			t.Errorf("IsClear(%q) = false, want true", s)
		}
	}
	for _, s := range []PaneStatus{StatusWorking, StatusUnread, StatusUnknown} {
		if IsClear(s) {
			t.Errorf("IsClear(%q) = true, want false", s)
		}
	}
}
