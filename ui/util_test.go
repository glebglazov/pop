package ui

import "testing"

func TestLastNSegments(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		n     int
		want  string
	}{
		{name: "depth 1", path: "/a/b/c/d", n: 1, want: "d"},
		{name: "depth 2", path: "/a/b/c/d", n: 2, want: "c/d"},
		{name: "depth 3", path: "/a/b/c/d", n: 3, want: "b/c/d"},
		{name: "depth exceeds segments", path: "/a/b", n: 5, want: "a/b"},
		{name: "depth 0 defaults to base", path: "/a/b/c", n: 0, want: "c"},
		{name: "depth negative defaults to base", path: "/a/b/c", n: -1, want: "c"},
		{name: "single segment", path: "/foo", n: 1, want: "foo"},
		{name: "single segment depth 2", path: "/foo", n: 2, want: "foo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LastNSegments(tt.path, tt.n)
			if got != tt.want {
				t.Errorf("LastNSegments(%q, %d) = %q, want %q", tt.path, tt.n, got, tt.want)
			}
		})
	}
}
