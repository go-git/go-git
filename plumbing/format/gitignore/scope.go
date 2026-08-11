package gitignore

import (
	"os"
	"slices"

	"github.com/go-git/go-billy/v6"
)

// Scope is the set of ignore patterns in effect for the entries of a single
// directory: those inherited from its ancestors, in ascending order of
// priority, followed by those the directory declares itself.
//
// A Scope also records whether the directory it belongs to is excluded. That
// is what a flat []Pattern cannot express. gitignore(5) states that a file
// cannot be re-included once a parent directory of it is excluded, so once a
// directory is excluded every entry below it is ignored whatever the patterns
// there say. Reference git tracks the same state in its walk rather than
// recomputing it per query: prep_exclude stops at the excluded directory and
// last_matching_pattern returns its pattern directly (dir.c).
//
// A Scope is immutable and safe for concurrent use. Descend derives the Scope
// for a subdirectory; the zero Scope matches nothing and is a valid root for a
// walk with no patterns at all.
type Scope struct {
	// patterns is ancestors-then-own, in ascending order of priority.
	patterns []Pattern
	matcher  Matcher
	// excluded reports whether this directory, or an ancestor of it, matched
	// an exclude rule.
	excluded bool
}

// NewScope returns the root Scope for a walk. base holds the patterns that
// apply to the whole tree before any .gitignore is read: typically
// .git/info/exclude, the core.excludesfile patterns, and any supplied by the
// caller. They are ordered by ascending priority, as for NewMatcher.
func NewScope(base []Pattern) *Scope {
	// Cloned so the Scope really is immutable, as documented above: without it
	// a caller mutating its own slice afterwards would change this Scope and
	// every Scope derived from it, including concurrently. Descend already
	// copies, so this is the only place the caller's memory could be shared.
	base = slices.Clone(base)
	return &Scope{patterns: base, matcher: NewMatcher(base)}
}

// Descend returns the Scope for the subdirectory of s at dir. dir is the full
// path of the subdirectory from the root of the walk, so that patterns keep
// matching against complete paths.
//
// readOwn supplies the patterns the subdirectory declares itself. It is called
// only when the subdirectory is not excluded, because git does not read ignore
// files below an excluded directory and nothing in one could change an
// outcome. Passing it as a function rather than a slice keeps that decision
// here, so callers cannot pay for a read that is then discarded. It may be nil
// when the subdirectory has no ignore file, which callers already know from
// the directory listing they hold.
func (s *Scope) Descend(dir []string, readOwn func() ([]Pattern, error)) (*Scope, error) {
	if s.excluded || s.matches(dir, true) {
		// Nothing below an excluded directory can change an outcome, so the
		// pattern set is frozen here and readOwn is never called.
		return &Scope{patterns: s.patterns, matcher: s.matcher, excluded: true}, nil
	}

	if readOwn == nil {
		return s, nil
	}

	own, err := readOwn()
	if err != nil {
		return nil, err
	}
	if len(own) == 0 {
		return s, nil
	}

	patterns := slices.Concat(s.patterns, own)
	return &Scope{patterns: patterns, matcher: NewMatcher(patterns)}, nil
}

// Excluded reports whether the directory this Scope belongs to, or an ancestor
// of it, is excluded. Every entry below such a directory is ignored.
func (s *Scope) Excluded() bool {
	return s.excluded
}

// Match reports whether path is excluded by the patterns in effect. path is an
// ordered sequence of the components of a complete path from the root of the
// walk, and isDir reports whether its final component is a directory.
//
// Unlike a Matcher built from a flat pattern list, Match honours excluded
// ancestors: below an excluded directory it reports true without consulting
// any pattern, so a negation there cannot re-include the entry.
func (s *Scope) Match(path []string, isDir bool) bool {
	if s.excluded {
		return true
	}
	return s.matches(path, isDir)
}

// Patterns returns the patterns in effect, ancestors first. The result must
// not be modified.
func (s *Scope) Patterns() []Pattern {
	return s.patterns
}

func (s *Scope) matches(path []string, isDir bool) bool {
	if s.matcher == nil {
		return false
	}
	return s.matcher.Match(path, isDir)
}

// DirPatterns returns the patterns declared by the .gitignore of a single
// directory, without recursing. A missing file is not an error and yields no
// patterns. Use it to build the readOwn argument of Scope.Descend while walking
// a tree; ReadPatterns is the eager whole-tree equivalent.
func DirPatterns(fs billy.Filesystem, path []string) ([]Pattern, error) {
	ps, err := readIgnoreFile(fs, path, gitignoreFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return ps, nil
}

// RootPatterns returns the patterns that apply to a whole worktree before any
// .gitignore is consulted: .git/info/exclude followed by the .gitignore at the
// root, in ascending order of priority. Missing files are not an error.
func RootPatterns(fs billy.Filesystem) ([]Pattern, error) {
	// Both files are opened directly rather than confirmed against a listing of
	// the root. Two opens cost less than enumerating a directory that may hold
	// thousands of entries, and the walk lists the root for its own purposes
	// anyway, so a listing here would be the second of two.
	//
	// Errors from the exclude file are dropped, as ReadPatterns drops them: it
	// is optional, and a worktree filesystem may refuse to open a path inside
	// .git outright rather than reporting it as missing.
	ps, _ := readIgnoreFile(fs, nil, infoExcludeFile)

	root, err := DirPatterns(fs, nil)
	if err != nil {
		return nil, err
	}

	return append(ps, root...), nil
}
