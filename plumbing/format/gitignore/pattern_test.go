package gitignore

import "testing"

func TestPatternSimpleMatch_inclusion(t *testing.T) {
	p := ParsePattern("!vul?ano", nil)
	if res := p.Match([]string{"value", "vulkano", "tail"}, false); res != Include {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternMatch_domainLonger_mismatch(t *testing.T) {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	if res := p.Match([]string{"head", "middle"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternMatch_domainSameLength_mismatch(t *testing.T) {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	if res := p.Match([]string{"head", "middle", "tail"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternMatch_domainMismatch_mismatch(t *testing.T) {
	p := ParsePattern("value", []string{"head", "middle", "tail"})
	if res := p.Match([]string{"head", "middle", "_tail_", "value"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternSimpleMatch_withDomain(t *testing.T) {
	p := ParsePattern("middle/", []string{"value", "volcano"})
	if res := p.Match([]string{"value", "volcano", "middle", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_onlyMatchInDomain_mismatch(t *testing.T) {
	p := ParsePattern("volcano/", []string{"value", "volcano"})
	if res := p.Match([]string{"value", "volcano", "tail"}, true); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternSimpleMatch_atStart(t *testing.T) {
	p := ParsePattern("value", nil)
	if res := p.Match([]string{"value", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_inTheMiddle(t *testing.T) {
	p := ParsePattern("value", nil)
	if res := p.Match([]string{"head", "value", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_atEnd(t *testing.T) {
	p := ParsePattern("value", nil)
	if res := p.Match([]string{"head", "value"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_atStart_dirWanted(t *testing.T) {
	p := ParsePattern("value/", nil)
	if res := p.Match([]string{"value", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_inTheMiddle_dirWanted(t *testing.T) {
	p := ParsePattern("value/", nil)
	if res := p.Match([]string{"head", "value", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_atEnd_dirWanted(t *testing.T) {
	p := ParsePattern("value/", nil)
	if res := p.Match([]string{"head", "value"}, true); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_atEnd_dirWanted_notADir_mismatch(t *testing.T) {
	p := ParsePattern("value/", nil)
	if res := p.Match([]string{"head", "value"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternSimpleMatch_mismatch(t *testing.T) {
	p := ParsePattern("value", nil)
	if res := p.Match([]string{"head", "val", "tail"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternSimpleMatch_valueLonger_mismatch(t *testing.T) {
	p := ParsePattern("val", nil)
	if res := p.Match([]string{"head", "value", "tail"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternSimpleMatch_withAsterisk(t *testing.T) {
	p := ParsePattern("v*o", nil)
	if res := p.Match([]string{"value", "vulkano", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_withQuestionMark(t *testing.T) {
	p := ParsePattern("vul?ano", nil)
	if res := p.Match([]string{"value", "vulkano", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_magicChars(t *testing.T) {
	p := ParsePattern("v[ou]l[kc]ano", nil)
	if res := p.Match([]string{"value", "volcano"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternSimpleMatch_wrongPattern_mismatch(t *testing.T) {
	p := ParsePattern("v[ou]l[", nil)
	if res := p.Match([]string{"value", "vol["}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_fromRootWithSlash(t *testing.T) {
	p := ParsePattern("/value/vul?ano", nil)
	if res := p.Match([]string{"value", "vulkano", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_withDomain(t *testing.T) {
	p := ParsePattern("middle/tail/", []string{"value", "volcano"})
	if res := p.Match([]string{"value", "volcano", "middle", "tail"}, true); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_onlyMatchInDomain_mismatch(t *testing.T) {
	p := ParsePattern("volcano/tail", []string{"value", "volcano"})
	if res := p.Match([]string{"value", "volcano", "tail"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_fromRootWithoutSlash(t *testing.T) {
	p := ParsePattern("value/vul?ano", nil)
	if res := p.Match([]string{"value", "vulkano", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_fromRoot_mismatch(t *testing.T) {
	p := ParsePattern("value/vulkano", nil)
	if res := p.Match([]string{"value", "volcano"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_fromRoot_tooShort_mismatch(t *testing.T) {
	p := ParsePattern("value/vul?ano", nil)
	if res := p.Match([]string{"value"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_fromRoot_notAtRoot_mismatch(t *testing.T) {
	p := ParsePattern("/value/volcano", nil)
	if res := p.Match([]string{"value", "value", "volcano"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_leadingAsterisks_atStart(t *testing.T) {
	p := ParsePattern("**/*lue/vol?ano", nil)
	if res := p.Match([]string{"value", "volcano", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_leadingAsterisks_notAtStart(t *testing.T) {
	p := ParsePattern("**/*lue/vol?ano", nil)
	if res := p.Match([]string{"head", "value", "volcano", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_leadingAsterisks_mismatch(t *testing.T) {
	p := ParsePattern("**/*lue/vol?ano", nil)
	if res := p.Match([]string{"head", "value", "Volcano", "tail"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_leadingAsterisks_isDir(t *testing.T) {
	p := ParsePattern("**/*lue/vol?ano/", nil)
	if res := p.Match([]string{"head", "value", "volcano", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_leadingAsterisks_isDirAtEnd(t *testing.T) {
	p := ParsePattern("**/*lue/vol?ano/", nil)
	if res := p.Match([]string{"head", "value", "volcano"}, true); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_leadingAsterisks_isDir_mismatch(t *testing.T) {
	p := ParsePattern("**/*lue/vol?ano/", nil)
	if res := p.Match([]string{"head", "value", "Colcano"}, true); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_leadingAsterisks_isDirNoDirAtEnd_mismatch(t *testing.T) {
	p := ParsePattern("**/*lue/vol?ano/", nil)
	if res := p.Match([]string{"head", "value", "volcano"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_tailingAsterisks(t *testing.T) {
	p := ParsePattern("/*lue/vol?ano/**", nil)
	if res := p.Match([]string{"value", "volcano", "tail", "moretail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_tailingAsterisks_exactMatch(t *testing.T) {
	p := ParsePattern("/*lue/vol?ano/**", nil)
	if res := p.Match([]string{"value", "volcano"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_middleAsterisks_emptyMatch(t *testing.T) {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	if res := p.Match([]string{"value", "volcano"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_middleAsterisks_oneMatch(t *testing.T) {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	if res := p.Match([]string{"value", "middle", "volcano"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_middleAsterisks_multiMatch(t *testing.T) {
	p := ParsePattern("/*lue/**/vol?ano", nil)
	if res := p.Match([]string{"value", "middle1", "middle2", "volcano"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_middleAsterisks_isDir_trailing(t *testing.T) {
	p := ParsePattern("/*lue/**/vol?ano/", nil)
	if res := p.Match([]string{"value", "middle1", "middle2", "volcano"}, true); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_middleAsterisks_isDir_trailing_mismatch(t *testing.T) {
	p := ParsePattern("/*lue/**/vol?ano/", nil)
	if res := p.Match([]string{"value", "middle1", "middle2", "volcano"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_middleAsterisks_isDir(t *testing.T) {
	p := ParsePattern("/*lue/**/vol?ano/", nil)
	if res := p.Match([]string{"value", "middle1", "middle2", "volcano", "tail"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_wrongDoubleAsterisk_mismatch(t *testing.T) {
	p := ParsePattern("/*lue/**foo/vol?ano", nil)
	if res := p.Match([]string{"value", "foo", "volcano", "tail"}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_magicChars(t *testing.T) {
	p := ParsePattern("**/head/v[ou]l[kc]ano", nil)
	if res := p.Match([]string{"value", "head", "volcano"}, false); res != Exclude {
		t.Errorf("expected Exclude, found %v", res)
	}
}

func TestPatternGlobMatch_wrongPattern_noTraversal_mismatch(t *testing.T) {
	p := ParsePattern("**/head/v[ou]l[", nil)
	if res := p.Match([]string{"value", "head", "vol["}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}

func TestPatternGlobMatch_wrongPattern_onTraversal_mismatch(t *testing.T) {
	p := ParsePattern("/value/**/v[ou]l[", nil)
	if res := p.Match([]string{"value", "head", "vol["}, false); res != NoMatch {
		t.Errorf("expected NoMatch, found %v", res)
	}
}
