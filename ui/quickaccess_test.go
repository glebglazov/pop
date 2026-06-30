package ui

import "testing"

func TestQuickAccessEnabled(t *testing.T) {
	tests := []struct {
		modifier string
		want     bool
	}{
		{"alt", true},
		{"ctrl", true},
		{"disabled", false},
		{"", true}, // defaults to alt
	}
	for _, tt := range tests {
		q := NewQuickAccess(tt.modifier)
		if got := q.Enabled(); got != tt.want {
			t.Errorf("NewQuickAccess(%q).Enabled() = %v, want %v", tt.modifier, got, tt.want)
		}
	}
}

func TestQuickAccessLabel(t *testing.T) {
	tests := []struct {
		modifier string
		n        int
		want     string
	}{
		{"alt", 1, "⌥1"},
		{"alt", 9, "⌥9"},
		{"ctrl", 3, "^3"},
		{"disabled", 1, "  "},
	}
	for _, tt := range tests {
		q := NewQuickAccess(tt.modifier)
		if got := q.Label(tt.n); got != tt.want {
			t.Errorf("NewQuickAccess(%q).Label(%d) = %q, want %q", tt.modifier, tt.n, got, tt.want)
		}
	}
}

func TestQuickAccessDigit(t *testing.T) {
	tests := []struct {
		name     string
		modifier string
		msg      KeyPress
		want     int
	}{
		{
			name:     "alt digit",
			modifier: "alt",
			msg:      KeyPress{Code: '5', Alt: true},
			want:     5,
		},
		{
			name:     "ctrl digit",
			modifier: "ctrl",
			msg:      KeyPress{Code: '2', Ctrl: true},
			want:     2,
		},
		{
			name:     "digit without modifier",
			modifier: "alt",
			msg:      KeyPress{Code: '3'},
			want:     0,
		},
		{
			name:     "wrong modifier",
			modifier: "alt",
			msg:      KeyPress{Code: '4', Ctrl: true},
			want:     0,
		},
		{
			name:     "disabled",
			modifier: "disabled",
			msg:      KeyPress{Code: '1', Alt: true},
			want:     0,
		},
		{
			name:     "non digit",
			modifier: "alt",
			msg:      KeyPress{Code: 'a', Alt: true},
			want:     0,
		},
		{
			name:     "zero digit",
			modifier: "alt",
			msg:      KeyPress{Code: '0', Alt: true},
			want:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NewQuickAccess(tt.modifier)
			if got := q.Digit(tt.msg); got != tt.want {
				t.Errorf("Digit(%+v) = %d, want %d", tt.msg, got, tt.want)
			}
		})
	}
}

func TestQuickAccessLabelFunc(t *testing.T) {
	q := NewQuickAccess("disabled")
	if q.LabelFunc() != nil {
		t.Fatal("disabled LabelFunc should be nil")
	}
	q = NewQuickAccess("alt")
	fn := q.LabelFunc()
	if fn == nil {
		t.Fatal("enabled LabelFunc should be non-nil")
	}
	if fn(2) != "⌥2" {
		t.Fatalf("LabelFunc(2) = %q, want ⌥2", fn(2))
	}
}
