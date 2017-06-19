package gitignore

import "testing"

func TestMatcher_Match(t *testing.T) {
	ps := []Pattern{
		ParsePattern("**/middle/v[uo]l?ano", nil),
		ParsePattern("!volcano", nil),
	}
	m := NewMatcher(ps)
	if !m.Match([]string{"head", "middle", "vulkano"}, false) {
		t.Errorf("expected a match, found mismatch")
	}
	if m.Match([]string{"head", "middle", "volcano"}, false) {
		t.Errorf("expected a mismatch, found a match")
	}
}
