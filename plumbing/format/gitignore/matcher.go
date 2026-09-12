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
	excluded, _ := m.matchForTraversal(path, isDir)
	return excluded
}

func (m *matcher) matchForTraversal(path []string, isDir bool) (bool, bool) {
	n := len(m.patterns)
	for i := n - 1; i >= 0; i-- {
		var match MatchResult
		var canPrune bool
		if p, ok := m.patterns[i].(*pattern); ok {
			match, canPrune = p.matchForTraversal(path, isDir)
		} else {
			match = m.patterns[i].Match(path, isDir)
			// Preserve the historical treatment of custom Pattern
			// implementations, for which no richer match information exists.
			canPrune = match == Exclude
		}
		if match > NoMatch {
			return match == Exclude, canPrune
		}
	}
	return false, false
}
