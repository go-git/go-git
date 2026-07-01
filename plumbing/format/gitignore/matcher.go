package gitignore

// Matcher defines a global multi-pattern matcher for gitignore patterns
type Matcher interface {
	// Match matches patterns in the order of priorities. As soon as an inclusion or
	// exclusion is found, not further matching is performed.
	Match(path []string, isDir bool) bool
}

// NewMatcher constructs a new global matcher. Patterns must be given in the order of
// increasing priority. That is most generic settings files first, then the content of
// the repo .gitignore, then content of .gitignore down the path or the repo and then
// the content command line arguments.
func NewMatcher(ps []Pattern) Matcher {
	return &matcher{ps}
}

type matcher struct {
	patterns []Pattern
}

func (m *matcher) Match(path []string, isDir bool) bool {
	n := len(m.patterns)
	for i := n - 1; i >= 0; i-- {
		if match := m.patterns[i].Match(path, isDir); match > NoMatch {
			return match == Exclude
		}
		// A directory-only inclusion such as !dir/ matches dir itself,
		// but it can also reopen descendants ignored only by dir-only rules.
		if dirOnlyInclusionMatchesAncestor(m.patterns[i], path) {
			var hasDirOnlyExclusion bool
			for j := i - 1; j >= 0; j-- {
				if match := m.patterns[j].Match(path, isDir); match == Exclude {
					if !isDirOnlyExclusion(m.patterns[j]) {
						return true
					}
					hasDirOnlyExclusion = true
				}
			}

			if hasDirOnlyExclusion {
				return false
			}
		}
	}
	return false
}

func dirOnlyInclusionMatchesAncestor(p Pattern, path []string) bool {
	pattern, ok := p.(*pattern)
	if !ok || !pattern.dirOnly || !pattern.inclusion {
		return false
	}

	for i := 1; i < len(path); i++ {
		if pattern.Match(path[:i], true) == Include {
			return true
		}
	}

	return false
}

func isDirOnlyExclusion(p Pattern) bool {
	pattern, ok := p.(*pattern)
	return ok && pattern.dirOnly && !pattern.inclusion
}
