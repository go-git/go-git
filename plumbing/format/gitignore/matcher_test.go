package gitignore

func (s *MatcherSuite) TestMatcher_Match() {
	ps := []Pattern{
		ParsePattern("**/middle/v[uo]l?ano", nil),
		ParsePattern("!volcano", nil),
	}

	m := NewMatcher(ps)
	s.True(m.Match([]string{"head", "middle", "vulkano"}, false))
	s.False(m.Match([]string{"head", "middle", "volcano"}, false))
}

// Test that the "exclude everything except" example
// from https://git-scm.com/docs/gitignore works
// (copied below):
//
//	$ cat .gitignore
//	# exclude everything except directory foo/bar
//	/*
//	!/foo
//	/foo/*
//	!/foo/bar
func (s *MatcherSuite) TestMatcher_EverythingExceptExample() {
	ps := []Pattern{
		ParsePattern("/*", nil),
		ParsePattern("!/foo", nil),
		ParsePattern("/foo/*", nil),
		ParsePattern("!/foo/bar", nil),
	}

	m := NewMatcher(ps)
	s.False(m.Match([]string{"foo"}, true))
	s.False(m.Match([]string{"foo", "bar"}, false))
	s.False(m.Match([]string{"foo", "bar"}, true))

	s.True(m.Match([]string{"baz"}, false))
	s.True(m.Match([]string{"baz"}, true))
	s.True(m.Match([]string{"baz", "foo"}, false))
	s.True(m.Match([]string{"baz", "foo"}, true))
	s.True(m.Match([]string{"foo", "baz"}, false))
	s.True(m.Match([]string{"foo", "baz"}, true))
}
