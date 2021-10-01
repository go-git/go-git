package url

import (
	"regexp"
)

var (
	isSchemeRegExp = regexp.MustCompile(`^[^:]+://`)
	// deprecated, backward compatibility.
	scpLikeUrlOldRegExp = regexp.MustCompile(`^(?:(?P<user>[^@]+)@)?(?P<host>[^:\s]+):(?:(?P<port>[0-9]{1,5})(?:\/|:))?(?P<path>[^\\].*\/[^\\].*)$`)
	scpLikeUrlNewRegExp = regexp.MustCompile(`^(?:(?P<user>[^@]+)@)?(?P<host>[^:\s]+):(?:(?P<port>[1-9][0-9]{1,4})(?::)?)?(?P<path>[^\\]+)$`)
)

// MatchesScheme returns true if the given string matches a URL-like
// format scheme.
func MatchesScheme(url string) bool {
	return isSchemeRegExp.MatchString(url)
}

// MatchesScpLike returns true if the given string matches an SCP-like
// format scheme.
func MatchesScpLike(url string) bool {
	return scpLikeUrlOldRegExp.MatchString(url)
}

// MatchesScpLikeExtended returns true if the given string matches an SCP-like
// format scheme.
func MatchesScpLikeExtended(url string) bool {
	return scpLikeUrlNewRegExp.MatchString(url)
}

// FindScpLikeComponents returns the user, host, port and path of the
// given SCP-like URL.
func FindScpLikeComponents(url string) (user, host, port, path string) {
	m := scpLikeUrlOldRegExp.FindStringSubmatch(url)
	return m[1], m[2], m[3], m[4]
}

// FindScpLikeComponentsExtended returns the user, host, port and path of the
// given SCP-like URL with correct repository relative path.
func FindScpLikeComponentsExtended(url string) (user, host, port, path string) {
	m := scpLikeUrlNewRegExp.FindStringSubmatch(url)
	return m[1], m[2], m[3], m[4]
}

// IsLocalEndpoint returns true if the given URL string specifies a
// local file endpoint.  For example, on a Linux machine,
// `/home/user/src/go-git` would match as a local endpoint, but
// `https://github.com/src-d/go-git` would not.
func IsLocalEndpoint(url string) bool {
	return !MatchesScheme(url) && !MatchesScpLike(url)
}
