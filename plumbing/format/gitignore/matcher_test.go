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
