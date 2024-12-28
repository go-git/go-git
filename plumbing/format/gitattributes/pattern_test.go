package gitattributes

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type PatternSuite struct {
	suite.Suite
}

func TestPatternSuite(t *testing.T) {
	suite.Run(t, new(PatternSuite))
}

func (s *PatternSuite) TestMatch_domainLonger_mismatch() {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	r := p.Match([]string{"head", "middle"})
	s.False(r)
}

func (s *PatternSuite) TestMatch_domainSameLength_mismatch() {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	r := p.Match([]string{"head", "middle", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestMatch_domainMismatch_mismatch() {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	r := p.Match([]string{"head", "middle", "_tail_", "value"})
	s.False(r)
}

func (s *PatternSuite) TestSimpleMatch_match() {
	p := ParsePattern("vul?ano", nil)
	r := p.Match([]string{"value", "vulkano"})
	s.True(r)
}

func (s *PatternSuite) TestSimpleMatch_withDomain() {
	p := ParsePattern("middle/tail", []string{"value", "volcano"})
	r := p.Match([]string{"value", "volcano", "middle", "tail"})
	s.True(r)
}

func (s *PatternSuite) TestSimpleMatch_onlyMatchInDomain_mismatch() {
	p := ParsePattern("value/volcano", []string{"value", "volcano"})
	r := p.Match([]string{"value", "volcano", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestSimpleMatch_atStart() {
	p := ParsePattern("value", nil)
	r := p.Match([]string{"value", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestSimpleMatch_inTheMiddle() {
	p := ParsePattern("value", nil)
	r := p.Match([]string{"head", "value", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestSimpleMatch_atEnd() {
	p := ParsePattern("value", nil)
	r := p.Match([]string{"head", "value"})
	s.True(r)
}

func (s *PatternSuite) TestSimpleMatch_mismatch() {
	p := ParsePattern("value", nil)
	r := p.Match([]string{"head", "val", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestSimpleMatch_valueLonger_mismatch() {
	p := ParsePattern("tai", nil)
	r := p.Match([]string{"head", "value", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestSimpleMatch_withAsterisk() {
	p := ParsePattern("t*l", nil)
	r := p.Match([]string{"value", "vulkano", "tail"})
	s.True(r)
}

func (s *PatternSuite) TestSimpleMatch_withQuestionMark() {
	p := ParsePattern("ta?l", nil)
	r := p.Match([]string{"value", "vulkano", "tail"})
	s.True(r)
}

func (s *PatternSuite) TestSimpleMatch_magicChars() {
	p := ParsePattern("v[ou]l[kc]ano", nil)
	r := p.Match([]string{"value", "volcano"})
	s.True(r)
}

func (s *PatternSuite) TestSimpleMatch_wrongPattern_mismatch() {
	p := ParsePattern("v[ou]l[", nil)
	r := p.Match([]string{"value", "vol["})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_fromRootWithSlash() {
	p := ParsePattern("/value/vul?ano/tail", nil)
	r := p.Match([]string{"value", "vulkano", "tail"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_withDomain() {
	p := ParsePattern("middle/tail", []string{"value", "volcano"})
	r := p.Match([]string{"value", "volcano", "middle", "tail"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_onlyMatchInDomain_mismatch() {
	p := ParsePattern("volcano/tail", []string{"value", "volcano"})
	r := p.Match([]string{"value", "volcano", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_fromRootWithoutSlash() {
	p := ParsePattern("value/vul?ano/tail", nil)
	r := p.Match([]string{"value", "vulkano", "tail"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_fromRoot_mismatch() {
	p := ParsePattern("value/vulkano", nil)
	r := p.Match([]string{"value", "volcano"})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_fromRoot_tooShort_mismatch() {
	p := ParsePattern("value/vul?ano", nil)
	r := p.Match([]string{"value"})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_fromRoot_notAtRoot_mismatch() {
	p := ParsePattern("/value/volcano", nil)
	r := p.Match([]string{"value", "value", "volcano"})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_atStart() {
	p := ParsePattern("**/*lue/vol?ano/ta?l", nil)
	r := p.Match([]string{"value", "volcano", "tail"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_notAtStart() {
	p := ParsePattern("**/*lue/vol?ano/tail", nil)
	r := p.Match([]string{"head", "value", "volcano", "tail"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_mismatch() {
	p := ParsePattern("**/*lue/vol?ano/tail", nil)
	r := p.Match([]string{"head", "value", "Volcano", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_tailingAsterisks() {
	p := ParsePattern("/*lue/vol?ano/**", nil)
	r := p.Match([]string{"value", "volcano", "tail", "moretail"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_tailingAsterisks_single() {
	p := ParsePattern("/*lue/**", nil)
	r := p.Match([]string{"value", "volcano"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_tailingAsterisk_single() {
	p := ParsePattern("/*lue/*", nil)
	r := p.Match([]string{"value", "volcano", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_tailingAsterisks_exactMatch() {
	p := ParsePattern("/*lue/vol?ano/**", nil)
	r := p.Match([]string{"value", "volcano"})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_emptyMatch() {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	r := p.Match([]string{"value", "volcano"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_oneMatch() {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	r := p.Match([]string{"value", "middle", "volcano"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_multiMatch() {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	r := p.Match([]string{"value", "middle1", "middle2", "volcano"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_wrongDoubleAsterisk_mismatch() {
	p := ParsePattern("/*lue/**foo/vol?ano/tail", nil)
	r := p.Match([]string{"value", "foo", "volcano", "tail"})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_magicChars() {
	p := ParsePattern("**/head/v[ou]l[kc]ano", nil)
	r := p.Match([]string{"value", "head", "volcano"})
	s.True(r)
}

func (s *PatternSuite) TestGlobMatch_wrongPattern_noTraversal_mismatch() {
	p := ParsePattern("**/head/v[ou]l[", nil)
	r := p.Match([]string{"value", "head", "vol["})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_wrongPattern_onTraversal_mismatch() {
	p := ParsePattern("/value/**/v[ou]l[", nil)
	r := p.Match([]string{"value", "head", "vol["})
	s.False(r)
}

func (s *PatternSuite) TestGlobMatch_issue_923() {
	p := ParsePattern("**/android/**/GeneratedPluginRegistrant.java", nil)
	r := p.Match([]string{"packages", "flutter_tools", "lib", "src", "android", "gradle.dart"})
	s.False(r)
}
