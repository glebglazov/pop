package repokey

import "testing"

func TestBasename(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"game_server-a1b2c3d4e5f6", "game_server"},
		{"my-cool-repo-abcdef012345", "my-cool-repo"},
		{"nodash", "nodash"},                       // no dash at all
		{"repo-short", "repo-short"},               // suffix not 12 chars
		{"repo-ghijklmnopqr", "repo-ghijklmnopqr"}, // 12 chars but non-hex
	}
	for _, c := range cases {
		if got := Basename(c.in); got != c.want {
			t.Errorf("Basename(%q) = %q, want %q", c.in, got, c.want)
		}
		if got, want := HasKeyShape(c.in), c.in != c.want; got != want {
			t.Errorf("HasKeyShape(%q) = %v, want %v", c.in, got, want)
		}
	}
}

func TestKeyRoundTrips(t *testing.T) {
	t.Parallel()
	key := Key("my-cool-repo", "abcdef012345")
	if key != "my-cool-repo-abcdef012345" {
		t.Fatalf("Key() = %q", key)
	}
	if got := Basename(key); got != "my-cool-repo" {
		t.Errorf("Basename(Key(...)) = %q, want %q", got, "my-cool-repo")
	}
}
