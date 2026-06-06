package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ReleaseFetcher queries the latest published Release tag of pop. It is the
// single network seam behind the Update check; everything downstream (CalVer
// comparison, dev-build detection) operates on the returned string and is
// pure, so tests inject a mock here and never touch the network.
type ReleaseFetcher interface {
	// LatestReleaseTag returns the tag name of the latest Release (e.g.
	// "v2026.6.1"), or an error if the lookup fails for any reason.
	LatestReleaseTag() (string, error)
}

// defaultReleaseAPIURL is the GitHub API endpoint for pop's latest Release.
const defaultReleaseAPIURL = "https://api.github.com/repos/glebglazov/pop/releases/latest"

// RealReleaseFetcher implements ReleaseFetcher against the GitHub releases API.
type RealReleaseFetcher struct {
	URL     string
	Timeout time.Duration
}

func NewRealReleaseFetcher() *RealReleaseFetcher {
	return &RealReleaseFetcher{URL: defaultReleaseAPIURL, Timeout: 5 * time.Second}
}

func (f *RealReleaseFetcher) LatestReleaseTag() (string, error) {
	url := f.URL
	if url == "" {
		url = defaultReleaseAPIURL
	}
	timeout := f.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("release API response had no tag_name")
	}
	return payload.TagName, nil
}
