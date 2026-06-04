package transport

import (
	"net/url"

	giturl "github.com/go-git/go-git/v6/internal/url"
)

// ParseURL parses a remote URL string into a *url.URL. It handles:
//   - Standard URLs (https://host/path, ssh://host/path, git://host/path)
//   - SCP-like URLs (git@host:path) — normalized to ssh:// scheme
//   - Local paths (/path/to/repo, C:\path) — normalized to file:// scheme
func ParseURL(endpoint string) (*url.URL, error) {
	if u, ok := giturl.ParseSCP(endpoint); ok {
		return u, nil
	}

	if u, ok := giturl.ParseFile(endpoint); ok {
		return u, nil
	}

	return giturl.ParseURL(endpoint)
}
