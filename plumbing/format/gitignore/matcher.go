package gitignore

// Matcher defines a global multi-pattern matcher for gitignore patterns.
type Matcher interface {
	// Match reports whether path is excluded by the highest-priority matching
	// pattern. Path is an ordered sequence of logical path components. Patterns
	// created with ParsePattern match only paths beginning with their domain.
	// isDir reports whether the final path component is a directory. For a
	// pattern ending in "/", isDir only restricts a match at the candidate
	// endpoint; descendants of a matched directory may still match.
	Match(path []string, isDir bool) bool
}

// NewMatcher constructs a new global matcher from patterns in increasing
// priority order. Match evaluates them from last to first and uses the first
// Exclude or Include result. Generic settings files should come first, followed
// by the repository .gitignore, .gitignore files in successively deeper
// directories, and command-line arguments.
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
