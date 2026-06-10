package config

import (
	"errors"
	"strings"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

var errURLEmptyInsteadOf = errors.New("url config: empty insteadOf")

// URL defines URL rewrite rules.
type URL struct {
	// Name new base url
	Name string
	// Any URL that starts with this value will be rewritten to start, instead, with <base>.
	// When more than one insteadOf strings match a given URL, the longest match is used.
	InsteadOfs []string
	// Any URL that starts with this value will be rewritten to start, instead, with <base>
	// for pushes only. This does not apply when a remote has an explicit pushurl.
	PushInsteadOfs []string

	// raw representation of the subsection, filled by marshal or unmarshal are
	// called.
	raw *format.Subsection
}

// Validate validates fields of branch.
func (u *URL) Validate() error {
	if len(u.InsteadOfs) == 0 && len(u.PushInsteadOfs) == 0 {
		return errURLEmptyInsteadOf
	}

	return nil
}

const (
	insteadOfKey     = "insteadOf"
	pushInsteadOfKey = "pushInsteadOf"
)

func (u *URL) unmarshal(s *format.Subsection) error {
	u.raw = s

	u.Name = s.Name
	u.InsteadOfs = u.raw.OptionAll(insteadOfKey)
	u.PushInsteadOfs = u.raw.OptionAll(pushInsteadOfKey)
	return nil
}

func (u *URL) marshal() *format.Subsection {
	if u.raw == nil {
		u.raw = &format.Subsection{}
	}

	u.raw.Name = u.Name
	u.raw.SetOption(insteadOfKey, u.InsteadOfs...)
	u.raw.SetOption(pushInsteadOfKey, u.PushInsteadOfs...)

	return u.raw
}

func rewriteLongestURLMatch(remoteURL string, urls map[string]*URL, prefixes func(*URL) []string) (string, bool) {
	var longestMatch *URL
	var longestPrefix string
	var longestMatchLength int

	for _, u := range urls {
		for _, currentPrefix := range prefixes(u) {
			if !strings.HasPrefix(remoteURL, currentPrefix) {
				continue
			}

			lengthCurrentPrefix := len(currentPrefix)

			// according to spec if there is more than one match, take the longest
			if longestMatch == nil || longestMatchLength < lengthCurrentPrefix {
				longestMatch = u
				longestPrefix = currentPrefix
				longestMatchLength = lengthCurrentPrefix
			}
		}
	}

	if longestMatch == nil {
		return remoteURL, false
	}

	return longestMatch.Name + remoteURL[len(longestPrefix):], true
}

// ApplyInsteadOf applies the URL rewrite rules to the given URL.
func (u *URL) ApplyInsteadOf(url string) string {
	return u.apply(url, u.InsteadOfs)
}

// ApplyPushInsteadOf applies the push URL rewrite rules to the given URL.
func (u *URL) ApplyPushInsteadOf(url string) string {
	return u.apply(url, u.PushInsteadOfs)
}

func (u *URL) apply(url string, prefixes []string) string {
	rewritten, _ := rewriteLongestURLMatch(url, map[string]*URL{u.Name: u}, func(*URL) []string {
		return prefixes
	})
	return rewritten
}
