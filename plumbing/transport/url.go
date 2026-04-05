package transport

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"runtime"
	"strings"

	giturl "github.com/go-git/go-git/v6/internal/url"
)

var fileIssueWindows = regexp.MustCompile(`^/[A-Za-z]:(/|\\)`)

// ParseURL parses a remote URL string into a *url.URL. It handles:
//   - Standard URLs (https://host/path, ssh://host/path, git://host/path)
//   - SCP-like URLs (git@host:path) — normalized to ssh:// scheme
//   - Local paths (/path/to/repo, C:\path) — normalized to file:// scheme
func ParseURL(endpoint string) (*url.URL, error) {
	if u, ok := parseSCPLike(endpoint); ok {
		return u, nil
	}

	if u, ok := parseFile(endpoint); ok {
		return u, nil
	}

	return parseURL(endpoint)
}

func parseURL(endpoint string) (*url.URL, error) {
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

func parseSCPLike(endpoint string) (*url.URL, bool) {
	if giturl.MatchesScheme(endpoint) || !giturl.MatchesScpLike(endpoint) {
		return nil, false
	}

	user, host, port, path := giturl.FindScpLikeComponents(endpoint)
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

func parseFile(endpoint string) (*url.URL, bool) {
	if giturl.MatchesScheme(endpoint) {
		return nil, false
	}

	return &url.URL{
		Scheme: "file",
		Path:   endpoint,
	}, true
}

