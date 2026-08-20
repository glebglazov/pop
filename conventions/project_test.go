package conventions

import "testing"

// TestRemoteSlugIsOneSegmentPerProject: the project rank is keyed by the remote
// so that a convention document follows the project rather than a clone, which
// only works if the URL forms one project is cloned by all reduce to one key.
func TestRemoteSlugIsOneSegmentPerProject(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{"git@github.com:tripledot/github_dashboard.git", "github.com-tripledot-github_dashboard"},
		{"https://github.com/tripledot/github_dashboard.git", "github.com-tripledot-github_dashboard"},
		{"https://github.com/tripledot/github_dashboard\n", "github.com-tripledot-github_dashboard"},
		{"ssh://git@gitlab.example.com:2222/group/sub/repo.git", "gitlab.example.com-2222-group-sub-repo"},
		{"/srv/git/bare-repo.git", "srv-git-bare-repo"},
		// Nothing usable is no key, and the caller falls back to identity.
		{"", ""},
		{"..", ""},
		{"///", ""},
	}
	for _, tt := range tests {
		if got := remoteSlug(tt.remote); got != tt.want {
			t.Errorf("remoteSlug(%q) = %q, want %q", tt.remote, got, tt.want)
		}
	}
}
