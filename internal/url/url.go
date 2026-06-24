// Package url provides URL parsing utilities for git endpoints.
package url

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"runtime"
	"strings"
)

var (
	isSchemeRegExp = regexp.MustCompile(`^[^:]+://`)

	// Ref: https://github.com/git/git/blob/v2.54.0/Documentation/urls.adoc#L41-L48
	scpLikeURLRegExp = regexp.MustCompile(`^(?:(?P<user>[^@]+)@)?(?P<host>[^:\s]+):(?:(?P<port>[0-9]{1,5}):)?(?P<path>[^\\].*)$`)

	fileIssueWindows = regexp.MustCompile(`^/[A-Za-z]:(/|\\)`)
)

// MatchesScheme returns true if the given string matches a URL-like
// format scheme.
func MatchesScheme(url string) bool {
	return isSchemeRegExp.MatchString(url)
}

// MatchesScpLike returns true if the given string matches an SCP-like
// format scheme.
func MatchesScpLike(url string) bool {
	if !scpLikeURLRegExp.MatchString(url) {
		return false
	}
	// Mirror canonical Git's url_is_local_not_ssh in connect.c[1] for
	// the cases the regex above cannot disambiguate by itself: a URL
	// is treated as a local path (not SCP-style SSH) when a `/`
	// precedes the first `:` (e.g. `./relative:path`,
	// `/abs/with:colon/file`), or — on Windows only — when it has a
	// DOS drive prefix like `C:foo` where the host is a single
	// ASCII letter.
	//
	// [1]: https://github.com/git/git/blob/v2.54.0/connect.c#L710-L716
	if before, _, _ := strings.Cut(url, ":"); strings.Contains(before, "/") {
		return false
	}
	if runtime.GOOS == "windows" && hasDosDrivePrefix(url) {
		return false
	}
	return true
}

// hasDosDrivePrefix reports whether s begins with `<letter>:` (a
// Windows drive prefix such as `C:` or `c:`). Mirrors canonical Git's
// win32_has_dos_drive_prefix[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/compat/win32/path-utils.c#L20-L29
func hasDosDrivePrefix(s string) bool {
	if len(s) < 2 || s[1] != ':' {
		return false
	}
	c := s[0]
	return ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z')
}

// FindScpLikeComponents returns the user, host, port and path of the
// given SCP-like URL.
func FindScpLikeComponents(url string) (user, host, port, path string) {
	m := scpLikeURLRegExp.FindStringSubmatch(url)
	return m[1], m[2], m[3], m[4]
}

// IsLocalEndpoint returns true if the given URL string specifies a
// local file endpoint.  For example, on a Linux machine,
// `/home/user/src/go-git` would match as a local endpoint, but
// `https://github.com/src-d/go-git` would not.
func IsLocalEndpoint(url string) bool {
	return !MatchesScheme(url) && !MatchesScpLike(url)
}

// Parse parses a remote URL string into a *url.URL. It handles:
//   - Standard URLs (https://host/path, ssh://host/path, git://host/path)
//   - SCP-like URLs (git@host:path) — normalized to ssh:// scheme
//   - Local paths (/path/to/repo, C:\path) — normalized to file:// scheme
func Parse(endpoint string) (*url.URL, error) {
	if u, ok := ParseSCP(endpoint); ok {
		return u, nil
	}

	if u, ok := ParseFile(endpoint); ok {
		return u, nil
	}

	return ParseURL(endpoint)
}

// ParseURL parses a standard URL string (e.g. https://host/path) into
// a *url.URL. It also handles file:// URLs with Windows path fixing.
// Returns an error if the URL is not absolute.
func ParseURL(endpoint string) (*url.URL, error) {
	if after, found := strings.CutPrefix(endpoint, "file://"); found {
		path := after
		if runtime.GOOS == "windows" && fileIssueWindows.MatchString(path) {
			path = path[1:]
		}
		return &url.URL{
			Scheme: "file",
			Path:   path,
		}, nil
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	if !u.IsAbs() {
		return nil, fmt.Errorf("invalid endpoint: %s", endpoint)
	}

	return u, nil
}

// ParseSCP parses an SCP-like URL (e.g. git@github.com:user/repo.git)
// into an ssh:// *url.URL. Returns the URL and true if the endpoint
// matches the SCP-like format, or nil and false otherwise.
func ParseSCP(endpoint string) (*url.URL, bool) {
	if MatchesScheme(endpoint) || !MatchesScpLike(endpoint) {
		return nil, false
	}

	user, host, port, path := FindScpLikeComponents(endpoint)
	if port != "" {
		host = net.JoinHostPort(host, port)
	}

	return &url.URL{
		Scheme: "ssh",
		User:   url.User(user),
		Host:   host,
		Path:   path,
	}, true
}

// ParseFile parses a local file path into a file:// *url.URL.
// Returns the URL and true if the endpoint has no scheme, or nil and
// false otherwise.
func ParseFile(endpoint string) (*url.URL, bool) {
	if MatchesScheme(endpoint) {
		return nil, false
	}

	return &url.URL{
		Scheme: "file",
		Path:   endpoint,
	}, true
}
