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

var fileIssueWindows = regexp.MustCompile(`^/[A-Za-z]:(/|\\)`)

// scpLikeWhitespace contains the ASCII whitespace characters matched by
// \s in a Go regular expression.
const scpLikeWhitespace = "\t\n\f\r "

// MatchesScheme reports whether the first colon in url follows at least
// one byte and is followed by "//". It does not validate the scheme.
func MatchesScheme(url string) bool {
	i := strings.IndexByte(url, ':')
	return i > 0 && strings.HasPrefix(url[i:], "://")
}

// matchScpLike splits s according to the following grammar:
//
//	^(?:(?P<user>[^@]+)@)?(?P<host>[^:\s]+):(?:(?P<port>[0-9]{1,5}):)?(?P<path>[^\\].*)$
//
// The optional user is tried before the form without a user. The optional
// port is tried before the form without a port.
// If s does not match, it returns empty components and false.
// It does not exclude URLs with schemes or local paths; see [ParseSCP].
func matchScpLike(s string) (user, host, port, path string, ok bool) {
	// `[^@]+` cannot contain an `@`, so the user can only ever be the
	// text preceding the first one, and must be non-empty.
	if at := strings.IndexByte(s, '@'); at > 0 {
		if host, port, path, ok := matchScpLikeAfterUser(s[at+1:]); ok {
			return s[:at], host, port, path, true
		}
	}
	if host, port, path, ok := matchScpLikeAfterUser(s); ok {
		return "", host, port, path, true
	}

	// On a non-match every component is empty, so a caller that ignores
	// ok cannot mistake a partial parse for a result.
	return "", "", "", "", false
}

// matchScpLikeAfterUser parses the host, optional port, and path in s.
// It applies the grammar used by matchScpLike after the optional user.
// The components are valid only when ok is true.
func matchScpLikeAfterUser(s string) (host, port, path string, ok bool) {
	// `[^:\s]+` cannot contain a `:`, so the host must end at the first
	// one, must be non-empty, and must hold no whitespace.
	colon := strings.IndexByte(s, ':')
	if colon <= 0 {
		return "", "", "", false
	}
	host = s[:colon]
	if strings.ContainsAny(host, scpLikeWhitespace) {
		return "", "", "", false
	}

	rest := s[colon+1:]
	// `[0-9]{1,5}` is greedy and cannot contain the `:` that closes it,
	// so the only candidate port is the full run of leading digits.
	if n := leadingDigits(rest); n >= 1 && n <= 5 && n < len(rest) && rest[n] == ':' {
		if path, ok := matchScpLikePath(rest[n+1:]); ok {
			return host, rest[:n], path, true
		}
	}
	path, ok = matchScpLikePath(rest)
	return host, "", path, ok
}

// matchScpLikePath returns s and true if s matches the SCP-like path grammar.
// The path must be non-empty and must not start with a backslash
// or contain a newline after its first rune. On a non-match, it returns an
// empty string and false.
func matchScpLikePath(s string) (string, bool) {
	if s == "" || s[0] == '\\' {
		return "", false
	}
	// `.` excludes `\n` and `$` is end of text, so a newline anywhere
	// past the first rune fails the match. `\n` is never a UTF-8
	// continuation byte, so scanning from the second byte is exact even
	// when the first rune is multi-byte.
	if strings.IndexByte(s[1:], '\n') >= 0 {
		return "", false
	}
	return s, true
}

// leadingDigits returns the length of the run of ASCII digits at the
// start of s.
func leadingDigits(s string) int {
	i := 0
	for i < len(s) && '0' <= s[i] && s[i] <= '9' {
		i++
	}
	return i
}

// MatchesScpLike returns true if the given string matches an SCP-like
// format scheme.
func MatchesScpLike(url string) bool {
	if _, _, _, _, ok := matchScpLike(url); !ok {
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

// FindScpLikeComponents returns the user, host, port, and path in url.
// If url does not match the SCP-like grammar, it returns empty components
// and false. It does not exclude URLs with schemes or local paths; callers
// should check [MatchesScheme] and [MatchesScpLike] first.
func FindScpLikeComponents(url string) (user, host, port, path string, ok bool) {
	return matchScpLike(url)
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

	user, host, port, path, ok := FindScpLikeComponents(endpoint)
	if !ok {
		return nil, false
	}

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
