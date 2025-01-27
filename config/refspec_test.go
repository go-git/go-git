package config

import (
	"fmt"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/suite"
)

type RefSpecSuite struct {
	suite.Suite
}

func TestRefSpecSuite(t *testing.T) {
	suite.Run(t, new(RefSpecSuite))
}

func (s *RefSpecSuite) TestRefSpecIsValid() {
	spec := RefSpec("+refs/heads/*:refs/remotes/origin/*")
	s.NoError(spec.Validate())

	spec = RefSpec("refs/heads/*:refs/remotes/origin/")
	s.ErrorIs(spec.Validate(), ErrRefSpecMalformedWildcard)

	spec = RefSpec("refs/heads/master:refs/remotes/origin/master")
	s.NoError(spec.Validate())

	spec = RefSpec(":refs/heads/master")
	s.NoError(spec.Validate())

	spec = RefSpec(":refs/heads/*")
	s.ErrorIs(spec.Validate(), ErrRefSpecMalformedWildcard)

	spec = RefSpec(":*")
	s.ErrorIs(spec.Validate(), ErrRefSpecMalformedWildcard)

	spec = RefSpec("refs/heads/*")
	s.ErrorIs(spec.Validate(), ErrRefSpecMalformedSeparator)

	spec = RefSpec("refs/heads:")
	s.ErrorIs(spec.Validate(), ErrRefSpecMalformedSeparator)

	spec = RefSpec("12039e008f9a4e3394f3f94f8ea897785cb09448:refs/heads/foo")
	s.NoError(spec.Validate())

	spec = RefSpec("12039e008f9a4e3394f3f94f8ea897785cb09448:refs/heads/*")
	s.ErrorIs(spec.Validate(), ErrRefSpecMalformedWildcard)
}

func (s *RefSpecSuite) TestRefSpecIsForceUpdate() {
	spec := RefSpec("+refs/heads/*:refs/remotes/origin/*")
	s.True(spec.IsForceUpdate())

	spec = RefSpec("refs/heads/*:refs/remotes/origin/*")
	s.False(spec.IsForceUpdate())
}

func (s *RefSpecSuite) TestRefSpecIsDelete() {
	spec := RefSpec(":refs/heads/master")
	s.True(spec.IsDelete())

	spec = RefSpec("+refs/heads/*:refs/remotes/origin/*")
	s.False(spec.IsDelete())

	spec = RefSpec("refs/heads/*:refs/remotes/origin/*")
	s.False(spec.IsDelete())
}

func (s *RefSpecSuite) TestRefSpecIsExactSHA1() {
	spec := RefSpec("foo:refs/heads/master")
	s.False(spec.IsExactSHA1())

	spec = RefSpec("12039e008f9a4e3394f3f94f8ea897785cb09448:refs/heads/foo")
	s.True(spec.IsExactSHA1())
}

func (s *RefSpecSuite) TestRefSpecSrc() {
	spec := RefSpec("refs/heads/*:refs/remotes/origin/*")
	s.Equal("refs/heads/*", spec.Src())

	spec = RefSpec("+refs/heads/*:refs/remotes/origin/*")
	s.Equal("refs/heads/*", spec.Src())

	spec = RefSpec(":refs/heads/master")
	s.Equal("", spec.Src())

	spec = RefSpec("refs/heads/love+hate:refs/heads/love+hate")
	s.Equal("refs/heads/love+hate", spec.Src())

	spec = RefSpec("+refs/heads/love+hate:refs/heads/love+hate")
	s.Equal("refs/heads/love+hate", spec.Src())
}

func (s *RefSpecSuite) TestRefSpecMatch() {
	spec := RefSpec("refs/heads/master:refs/remotes/origin/master")
	s.False(spec.Match(plumbing.ReferenceName("refs/heads/foo")))
	s.True(spec.Match(plumbing.ReferenceName("refs/heads/master")))

	spec = RefSpec("+refs/heads/master:refs/remotes/origin/master")
	s.False(spec.Match(plumbing.ReferenceName("refs/heads/foo")))
	s.True(spec.Match(plumbing.ReferenceName("refs/heads/master")))

	spec = RefSpec(":refs/heads/master")
	s.True(spec.Match(plumbing.ReferenceName("")))
	s.False(spec.Match(plumbing.ReferenceName("refs/heads/master")))

	spec = RefSpec("refs/heads/love+hate:heads/love+hate")
	s.True(spec.Match(plumbing.ReferenceName("refs/heads/love+hate")))

	spec = RefSpec("+refs/heads/love+hate:heads/love+hate")
	s.True(spec.Match(plumbing.ReferenceName("refs/heads/love+hate")))
}

func (s *RefSpecSuite) TestRefSpecMatchGlob() {
	tests := map[string]map[string]bool{
		"refs/heads/*:refs/remotes/origin/*": {
			"refs/tag/foo":   false,
			"refs/heads/foo": true,
		},
		"refs/heads/*bc:refs/remotes/origin/*bc": {
			"refs/heads/abc": true,
			"refs/heads/bc":  true,
			"refs/heads/abx": false,
		},
		"refs/heads/a*c:refs/remotes/origin/a*c": {
			"refs/heads/abc": true,
			"refs/heads/ac":  true,
			"refs/heads/abx": false,
		},
		"refs/heads/ab*:refs/remotes/origin/ab*": {
			"refs/heads/abc": true,
			"refs/heads/ab":  true,
			"refs/heads/xbc": false,
		},
	}

	for specStr, data := range tests {
		spec := RefSpec(specStr)
		for ref, matches := range data {
			s.Equal(matches,
				spec.Match(plumbing.ReferenceName(ref)),
				fmt.Sprintf("while matching spec %q against ref %q", specStr, ref),
			)
		}
	}
}

func (s *RefSpecSuite) TestRefSpecDst() {
	spec := RefSpec("refs/heads/master:refs/remotes/origin/master")
	s.Equal("refs/remotes/origin/master",
		spec.Dst(plumbing.ReferenceName("refs/heads/master")).String())
}

func (s *RefSpecSuite) TestRefSpecDstBlob() {
	ref := "refs/heads/abc"
	tests := map[string]string{
		"refs/heads/*:refs/remotes/origin/*":       "refs/remotes/origin/abc",
		"refs/heads/*bc:refs/remotes/origin/*":     "refs/remotes/origin/a",
		"refs/heads/*bc:refs/remotes/origin/*bc":   "refs/remotes/origin/abc",
		"refs/heads/a*c:refs/remotes/origin/*":     "refs/remotes/origin/b",
		"refs/heads/a*c:refs/remotes/origin/a*c":   "refs/remotes/origin/abc",
		"refs/heads/ab*:refs/remotes/origin/*":     "refs/remotes/origin/c",
		"refs/heads/ab*:refs/remotes/origin/ab*":   "refs/remotes/origin/abc",
		"refs/heads/*abc:refs/remotes/origin/*abc": "refs/remotes/origin/abc",
		"refs/heads/abc*:refs/remotes/origin/abc*": "refs/remotes/origin/abc",
		// for these two cases, git specifically logs:
		// error: * Ignoring funny ref 'refs/remotes/origin/' locally
		// and ignores the ref; go-git does not currently do this validation,
		// but probably should.
		// "refs/heads/*abc:refs/remotes/origin/*": "",
		// "refs/heads/abc*:refs/remotes/origin/*": "",
	}

	for specStr, dst := range tests {
		spec := RefSpec(specStr)
		s.Equal(dst,
			spec.Dst(plumbing.ReferenceName(ref)).String(),
			fmt.Sprintf("while getting dst from spec %q with ref %q", specStr, ref),
		)
	}
}

func (s *RefSpecSuite) TestRefSpecReverse() {
	spec := RefSpec("refs/heads/*:refs/remotes/origin/*")
	s.Equal(RefSpec("refs/remotes/origin/*:refs/heads/*"), spec.Reverse())
}

func (s *RefSpecSuite) TestMatchAny() {
	specs := []RefSpec{
		"refs/heads/bar:refs/remotes/origin/foo",
		"refs/heads/foo:refs/remotes/origin/bar",
	}

	s.True(MatchAny(specs, plumbing.ReferenceName("refs/heads/foo")))
	s.True(MatchAny(specs, plumbing.ReferenceName("refs/heads/bar")))
	s.False(MatchAny(specs, plumbing.ReferenceName("refs/heads/master")))
}
