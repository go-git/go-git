package config

import (
	"errors"
	"strings"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

var errURLEmptyInsteadOf = errors.New("url config: empty insteadOf")

// Url defines Url rewrite rules
type URL struct {
	// Name new base url
	Name string
	// Any URL that starts with this value will be rewritten to start, instead, with <base>.
	// When more than one insteadOf strings match a given URL, the longest match is used.
	InsteadOfs []string

	// raw representation of the subsection, filled by marshal or unmarshal are
	// called.
	raw *format.Subsection
}

// Validate validates fields of branch
func (b *URL) Validate() error {
	if len(b.InsteadOfs) == 0 {
		return errURLEmptyInsteadOf
	}

	return nil
}

const (
	insteadOfKey = "insteadOf"
)

func (u *URL) unmarshal(s *format.Subsection) error {
	u.raw = s

	u.Name = s.Name
	u.InsteadOfs = u.raw.OptionAll(insteadOfKey)
	return nil
}

func (u *URL) marshal() *format.Subsection {
	if u.raw == nil {
		u.raw = &format.Subsection{}
	}

	u.raw.Name = u.Name
	u.raw.SetOption(insteadOfKey, u.InsteadOfs...)

	return u.raw
}

func findLongestInsteadOfMatch(remoteURL string, urls map[string]*URL) *URL {
	var longestMatch *URL
	var longestMatchLength int

	for _, u := range urls {
		for _, currentInsteadOf := range u.InsteadOfs {
			if !strings.HasPrefix(remoteURL, currentInsteadOf) {
				continue
			}

			lengthCurrentInsteadOf := len(currentInsteadOf)

			// according to spec if there is more than one match, take the longest
			if longestMatch == nil || longestMatchLength < lengthCurrentInsteadOf {
				longestMatch = u
				longestMatchLength = lengthCurrentInsteadOf
			}
		}
	}

	return longestMatch
}

func (u *URL) ApplyInsteadOf(url string) string {
	for _, j := range u.InsteadOfs {
		if strings.HasPrefix(url, j) {
			return u.Name + url[len(j):]
		}
	}

	return url
}
