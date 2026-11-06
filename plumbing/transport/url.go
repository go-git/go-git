package transport

import (
	"net/url"

	giturl "github.com/go-git/go-git/v6/internal/url"
)

// ParseURL parses a remote URL string into a *url.URL. It handles:
//   - Remote helper URLs (<transport>::<address>) — the transport becomes
//     the scheme and String returns the endpoint unchanged
//   - Standard URLs (https://host/path, ssh://host/path, git://host/path)
//   - SCP-like URLs (git@host:path) — normalized to ssh:// scheme
//   - Local paths (/path/to/repo, C:\path) — normalized to file:// scheme
//
// No transport is built in for remote helper URLs, even when the helper name
// matches a built-in scheme such as file or https. A client resolves them
// only when a transport is registered for the scheme with
// client.WithTransport.
func ParseURL(endpoint string) (*url.URL, error) {
	if u, ok := giturl.ParseRemoteHelper(endpoint); ok {
		return u, nil
	}

	if u, ok := giturl.ParseSCP(endpoint); ok {
		return u, nil
	}

	if u, ok := giturl.ParseFile(endpoint); ok {
		return u, nil
	}

	return giturl.ParseURL(endpoint)
}
