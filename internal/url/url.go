// Package url provides URL parsing utilities for git endpoints.
package url

import (
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var fileIssueWindows = regexp.MustCompile(`^/[A-Za-z]:(/|\\)`)

// MatchesScheme reports whether url contains "://".
// It does not validate the scheme or require it to be non-empty.
func MatchesScheme(url string) bool {
	return strings.Contains(url, "://")
}

// matchScpLike locates the path using Git's host_end and parse_connect_url
// rules before splitting the user from the host. Brackets are retained here;
// ParseSCP handles Git's subsequent unwrapping and port extraction.
func matchScpLike(s string) (user, host, path string, ok bool) {
	start, end := scpHostBounds(s)
	separatorStart := max(end, 0)
	colon := strings.IndexByte(s[separatorStart:], ':')
	if colon < 0 {
		return "", "", "", false
	}
	colon += separatorStart
	host, path = s[:colon], s[colon+1:]

	// Git calls host_end again on just the authority. Reuse the bounds
	// unless an unterminated @[ in the path hid the bracketed host.
	if start > colon {
		start, end = scpHostBounds(host)
	}
	if end >= 0 {
		if start > 0 {
			user = host[:start-1]
		}
		return user, host[start : end+1], path, true
	}
	// OpenSSH separates user and host at the last @, after Git has
	// isolated the authority from the path.
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		user, host = host[:at], host[at+1:]
	}
	return user, host, path, true
}

// scpHostBounds follows Git's host_end bracket precedence. A negative end
// means there is no complete bracketed host.
func scpHostBounds(s string) (start, end int) {
	if at := strings.Index(s, "@["); at >= 0 {
		start = at + 1
	}
	if start < len(s) && s[start] == '[' {
		if end := strings.IndexByte(s[start+1:], ']'); end >= 0 {
			return start, start + 1 + end
		}
	}
	return start, -1
}

// MatchesScpLike returns true if the given string matches an SCP-like
// format scheme.
func MatchesScpLike(url string) bool {
	if _, _, _, ok := matchScpLike(url); !ok {
		return false
	}
	// Mirror canonical Git's url_is_local_not_ssh in url.c[1] for the
	// cases the grammar above cannot disambiguate by itself: an
	// endpoint is a local path, not SCP-style SSH, when a `/` precedes
	// the first `:` (e.g. `./relative:path`, `/abs/with:colon/file`),
	// or — on Windows only — when it has a DOS drive prefix like
	// `C:foo` or `C:\repo`.
	//
	// The platform gate is Git's, not an approximation of it:
	// has_dos_drive_prefix is a no-op everywhere but Windows
	// (git-compat-util.h), and it is the only route to the local
	// reading for a drive-letter endpoint. So `C:\repo` is a path on
	// Windows and an SSH request to the host `C` on a Linux or macOS
	// host, in Git and here alike.
	//
	// [1]: https://github.com/git/git/blob/v2.56.0/url.c#L136-L142
	if before, _, _ := strings.Cut(url, ":"); strings.Contains(before, "/") {
		return false
	}
	if runtime.GOOS == "windows" && hasDosDrivePrefix(url) {
		return false
	}
	return true
}

// hasDosDrivePrefix reports whether s starts with an ASCII letter followed
// by a colon, as in "C:" or "c:".
func hasDosDrivePrefix(s string) bool {
	if len(s) < 2 || s[1] != ':' {
		return false
	}
	c := s[0]
	return ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z')
}

// FindScpLikeComponents returns the user, host, and path in url.
// If url does not match the SCP-like grammar, it returns empty components
// and false. It does not exclude URLs with schemes or local paths; callers
// should check [MatchesScheme] and [MatchesScpLike] first.
//
// A bracketed host retains its brackets, as in "[fe80::1]:repo.git".
// The path starts after the colon separating it from the host; subsequent
// colons are part of the path. ParseSCP extracts a port from a bracketed host
// when Git would do so.
func FindScpLikeComponents(url string) (user, host, path string, ok bool) {
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

	user, host, path, ok := FindScpLikeComponents(endpoint)
	if !ok {
		return nil, false
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		// Git's get_port checks the first colon after unwrapping the
		// authority. A colon in the user prevents a numeric suffix;
		// otherwise a suffix in 0..65535 is a port. See get_port in
		// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/connect.c.
		literal := host[1 : len(host)-1]
		var port string
		if colon := strings.IndexByte(literal, ':'); colon >= 0 && !strings.Contains(user, ":") {
			// strtol permits leading ASCII whitespace and a sign.
			// Multiple colons denote a host, not a numeric port.
			text := strings.TrimLeft(literal[colon+1:], " \t\n\r\v\f")
			if !strings.Contains(text, ":") {
				if n, err := strconv.ParseInt(text, 10, 32); err == nil && n >= 0 && n < 65536 {
					literal, port = literal[:colon], strconv.FormatInt(n, 10)
				}
			}
		}
		if at := strings.LastIndexByte(literal, '@'); at >= 0 {
			if user == "" {
				user = literal[:at]
			} else {
				user += "@" + literal[:at]
			}
			literal = literal[at+1:]
			host = "[" + literal + "]"
		}
		if port != "" {
			host = literal + ":" + port
		}
		// Otherwise retain the original brackets, so net/url cannot
		// reinterpret a colon in the host as a port.
	}

	u := &url.URL{
		Scheme: "ssh",
		Host:   host,
		Path:   path,
	}
	// The user is optional in this form, and url.User("") is not the
	// same as no user at all: it is an empty userinfo, which String
	// writes out as a bare `@` and which every `URL.User != nil` test
	// reads as "a user was given".
	if user != "" {
		u.User = url.User(user)
	}
	return u, true
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
