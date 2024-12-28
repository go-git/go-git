package gitignore

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

func (s *PatternSuite) TestSimpleMatch_inclusion() {
	p := ParsePattern("!vul?ano", nil)
	r := p.Match([]string{"value", "vulkano", "tail"}, false)
	s.Equal(Include, r)
}

func (s *PatternSuite) TestMatch_domainLonger_mismatch() {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	r := p.Match([]string{"head", "middle"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestMatch_domainSameLength_mismatch() {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	r := p.Match([]string{"head", "middle", "tail"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestMatch_domainMismatch_mismatch() {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	r := p.Match([]string{"head", "middle", "_tail_", "value"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestSimpleMatch_withDomain() {
	p := ParsePattern("middle/", []string{"value", "volcano"})
	r := p.Match([]string{"value", "volcano", "middle", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_onlyMatchInDomain_mismatch() {
	p := ParsePattern("volcano/", []string{"value", "volcano"})
	r := p.Match([]string{"value", "volcano", "tail"}, true)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestSimpleMatch_atStart() {
	p := ParsePattern("value", nil)
	r := p.Match([]string{"value", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_inTheMiddle() {
	p := ParsePattern("value", nil)
	r := p.Match([]string{"head", "value", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_atEnd() {
	p := ParsePattern("value", nil)
	r := p.Match([]string{"head", "value"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_atStart_dirWanted() {
	p := ParsePattern("value/", nil)
	r := p.Match([]string{"value", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_inTheMiddle_dirWanted() {
	p := ParsePattern("value/", nil)
	r := p.Match([]string{"head", "value", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_atEnd_dirWanted() {
	p := ParsePattern("value/", nil)
	r := p.Match([]string{"head", "value"}, true)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_atEnd_dirWanted_notADir_mismatch() {
	p := ParsePattern("value/", nil)
	r := p.Match([]string{"head", "value"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestSimpleMatch_mismatch() {
	p := ParsePattern("value", nil)
	r := p.Match([]string{"head", "val", "tail"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestSimpleMatch_valueLonger_mismatch() {
	p := ParsePattern("val", nil)
	r := p.Match([]string{"head", "value", "tail"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestSimpleMatch_withAsterisk() {
	p := ParsePattern("v*o", nil)
	r := p.Match([]string{"value", "vulkano", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_withQuestionMark() {
	p := ParsePattern("vul?ano", nil)
	r := p.Match([]string{"value", "vulkano", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_magicChars() {
	p := ParsePattern("v[ou]l[kc]ano", nil)
	r := p.Match([]string{"value", "volcano"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestSimpleMatch_wrongPattern_mismatch() {
	p := ParsePattern("v[ou]l[", nil)
	r := p.Match([]string{"value", "vol["}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_fromRootWithSlash() {
	p := ParsePattern("/value/vul?ano", nil)
	r := p.Match([]string{"value", "vulkano", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_withDomain() {
	p := ParsePattern("middle/tail/", []string{"value", "volcano"})
	r := p.Match([]string{"value", "volcano", "middle", "tail"}, true)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_onlyMatchInDomain_mismatch() {
	p := ParsePattern("volcano/tail", []string{"value", "volcano"})
	r := p.Match([]string{"value", "volcano", "tail"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_fromRootWithoutSlash() {
	p := ParsePattern("value/vul?ano", nil)
	r := p.Match([]string{"value", "vulkano", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_fromRoot_mismatch() {
	p := ParsePattern("value/vulkano", nil)
	r := p.Match([]string{"value", "volcano"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_fromRoot_tooShort_mismatch() {
	p := ParsePattern("value/vul?ano", nil)
	r := p.Match([]string{"value"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_fromRoot_notAtRoot_mismatch() {
	p := ParsePattern("/value/volcano", nil)
	r := p.Match([]string{"value", "value", "volcano"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_atStart() {
	p := ParsePattern("**/*lue/vol?ano", nil)
	r := p.Match([]string{"value", "volcano", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_notAtStart() {
	p := ParsePattern("**/*lue/vol?ano", nil)
	r := p.Match([]string{"head", "value", "volcano", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_mismatch() {
	p := ParsePattern("**/*lue/vol?ano", nil)
	r := p.Match([]string{"head", "value", "Volcano", "tail"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_isDir() {
	p := ParsePattern("**/*lue/vol?ano/", nil)
	r := p.Match([]string{"head", "value", "volcano", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_isDirAtEnd() {
	p := ParsePattern("**/*lue/vol?ano/", nil)
	r := p.Match([]string{"head", "value", "volcano"}, true)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_isDir_mismatch() {
	p := ParsePattern("**/*lue/vol?ano/", nil)
	r := p.Match([]string{"head", "value", "Colcano"}, true)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_leadingAsterisks_isDirNoDirAtEnd_mismatch() {
	p := ParsePattern("**/*lue/vol?ano/", nil)
	r := p.Match([]string{"head", "value", "volcano"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_tailingAsterisks() {
	p := ParsePattern("/*lue/vol?ano/**", nil)
	r := p.Match([]string{"value", "volcano", "tail", "moretail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_tailingAsterisks_exactMatch() {
	p := ParsePattern("/*lue/vol?ano/**", nil)
	r := p.Match([]string{"value", "volcano"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_emptyMatch() {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	r := p.Match([]string{"value", "volcano"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_oneMatch() {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	r := p.Match([]string{"value", "middle", "volcano"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_multiMatch() {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	r := p.Match([]string{"value", "middle1", "middle2", "volcano"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_isDir_trailing() {
	p := ParsePattern("/*lue/**/vol?ano/", nil)
	r := p.Match([]string{"value", "middle1", "middle2", "volcano"}, true)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_isDir_trailing_mismatch() {
	p := ParsePattern("/*lue/**/vol?ano/", nil)
	r := p.Match([]string{"value", "middle1", "middle2", "volcano"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_middleAsterisks_isDir() {
	p := ParsePattern("/*lue/**/vol?ano/", nil)
	r := p.Match([]string{"value", "middle1", "middle2", "volcano", "tail"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_wrongDoubleAsterisk_mismatch() {
	p := ParsePattern("/*lue/**foo/vol?ano", nil)
	r := p.Match([]string{"value", "foo", "volcano", "tail"}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_magicChars() {
	p := ParsePattern("**/head/v[ou]l[kc]ano", nil)
	r := p.Match([]string{"value", "head", "volcano"}, false)
	s.Equal(Exclude, r)
}

func (s *PatternSuite) TestGlobMatch_wrongPattern_noTraversal_mismatch() {
	p := ParsePattern("**/head/v[ou]l[", nil)
	r := p.Match([]string{"value", "head", "vol["}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_wrongPattern_onTraversal_mismatch() {
	p := ParsePattern("/value/**/v[ou]l[", nil)
	r := p.Match([]string{"value", "head", "vol["}, false)
	s.Equal(NoMatch, r)
}

func (s *PatternSuite) TestGlobMatch_issue_923() {
	p := ParsePattern("**/android/**/GeneratedPluginRegistrant.java", nil)
	r := p.Match([]string{"packages", "flutter_tools", "lib", "src", "android", "gradle.dart"}, false)
	s.Equal(NoMatch, r)
}
