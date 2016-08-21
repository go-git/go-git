package config

import (
	"strings"

	"gopkg.in/src-d/go-git.v4/core"
)

const (
	refSpecWildcard  = "*"
	refSpecForce     = "+"
	refSpecSeparator = ":"
)

// RefSpec is a mapping from local branches to remote references
// The format of the refspec is an optional +, followed by <src>:<dst>, where
// <src> is the pattern for references on the remote side and <dst> is where
// those references will be written locally. The + tells Git to update the
// reference even if it isn’t a fast-forward.
// eg.: "+refs/heads/*:refs/remotes/origin/*"
//
// https://git-scm.com/book/es/v2/Git-Internals-The-Refspec
type RefSpec string

// IsValid validates the RefSpec
func (s RefSpec) IsValid() bool {
	spec := string(s)
	if strings.Count(spec, refSpecSeparator) != 1 {
		return false
	}

	sep := strings.Index(spec, refSpecSeparator)
	if sep == len(spec) {
		return false
	}

	ws := strings.Count(spec[0:sep], refSpecWildcard)
	wd := strings.Count(spec[sep+1:len(spec)], refSpecWildcard)
	return ws == wd && ws < 2 && wd < 2
}

// IsForceUpdate returns if update is allowed in non fast-forward merges
func (s RefSpec) IsForceUpdate() bool {
	if s[0] == refSpecForce[0] {
		return true
	}

	return false
}

// Src return the src side
func (s RefSpec) Src() string {
	spec := string(s)
	start := strings.Index(spec, refSpecForce) + 1
	end := strings.Index(spec, refSpecSeparator)

	return spec[start:end]
}

// Match match the given core.ReferenceName against the source
func (s RefSpec) Match(n core.ReferenceName) bool {
	if !s.isGlob() {
		return s.matchExact(n)
	}

	return s.matchGlob(n)
}

func (s RefSpec) isGlob() bool {
	return strings.Index(string(s), refSpecWildcard) != -1
}

func (s RefSpec) matchExact(n core.ReferenceName) bool {
	return s.Src() == n.String()
}

func (s RefSpec) matchGlob(n core.ReferenceName) bool {
	src := s.Src()
	name := n.String()
	wildcard := strings.Index(src, refSpecWildcard)

	var prefix, suffix string
	prefix = src[0:wildcard]
	if len(src) < wildcard {
		suffix = src[wildcard+1 : len(suffix)]
	}

	return len(name) > len(prefix)+len(suffix) &&
		strings.HasPrefix(name, prefix) &&
		strings.HasSuffix(name, suffix)
}

// Dst returns the destination for the given remote reference
func (s RefSpec) Dst(n core.ReferenceName) core.ReferenceName {
	spec := string(s)
	start := strings.Index(spec, refSpecSeparator) + 1
	dst := spec[start:len(spec)]
	src := s.Src()

	if !s.isGlob() {
		return core.ReferenceName(dst)
	}

	name := n.String()
	ws := strings.Index(src, refSpecWildcard)
	wd := strings.Index(dst, refSpecWildcard)
	match := name[ws : len(name)-(len(src)-(ws+1))]

	return core.ReferenceName(dst[0:wd] + match + dst[wd+1:len(dst)])
}

func (s RefSpec) String() string {
	return string(s)
}

// MatchAny returns true if any of the RefSpec match with the given ReferenceName
func MatchAny(l []RefSpec, n core.ReferenceName) bool {
	for _, r := range l {
		if r.Match(n) {
			return true
		}
	}

	return false
}
