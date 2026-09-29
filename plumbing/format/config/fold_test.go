package config

import (
	"math/rand"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type FoldSuite struct {
	suite.Suite
}

func TestFoldSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(FoldSuite))
}

// foldCorpus holds names that exercise the corners of case folding: plain
// ASCII, the orbits that make strings.ToLower an unusable key (U+017F folds
// to 's', U+212A to 'k'), runes whose folding is not a simple case pair, and
// invalid UTF-8, which strings.EqualFold decodes as U+FFFD.
//
// The runes that matter are written as escapes on purpose: U+212A and an
// ASCII 'K' are indistinguishable in an editor or a diff, so spelling them
// out is the only way the corpus stays readable and survives a round trip
// through a tool that normalises source.
//
// Names holding invalid UTF-8 cannot reach foldKey through Decode, as gcfg's
// scanner rejects them with "illegal UTF-8 encoding" before the callback runs,
// but foldKey stands in for strings.EqualFold, so it has to agree with it
// there too.
var foldCorpus = []string{
	"", "a", "A", "b", "aa", "ab",
	"core", "CORE", "Core", "cOrE",
	"s", "S", "\u017f", "s\u017f", "\u017fs",
	"k", "K", "\u212a", "k\u212a",
	"\u00df", "\u1e9e", "ss", "SS",
	"i", "I", "\u0130", "\u0131",
	"\u03c3", "\u03c2", "\u03a3",
	"\u00e4", "\u00c4", "\U00010490", "\U00010490a",
	"\U0001043c", "\U00010414",
	"\xff", "\xfe", "\ufffd", "a\xffb", "a\ufffdb", "\xff\xff",
}

// TestFoldKeyMatchesEqualFold is the contract the decoder index relies on:
// two names share a key exactly when strings.EqualFold considers them equal.
func (s *FoldSuite) TestFoldKeyMatchesEqualFold() {
	for _, a := range foldCorpus {
		for _, b := range foldCorpus {
			s.Equal(strings.EqualFold(a, b), foldKey(a) == foldKey(b),
				"foldKey(%q) == foldKey(%q)", a, b)
		}
	}
}

// TestFoldKeyMatchesEqualFoldRandom covers combinations the fixed corpus does
// not, by pairing up random names drawn from an alphabet of runes that are
// known to fold into each other.
func (s *FoldSuite) TestFoldKeyMatchesEqualFoldRandom() {
	alphabet := []string{
		"a", "A", "s", "S", "\u017f", "k", "\u212a",
		"\u00e4", "\u00c4", "\U00010490", "-", "\xff", "\ufffd",
	}

	rnd := rand.New(rand.NewSource(42))
	name := func() string {
		var b strings.Builder
		for i := rnd.Intn(5); i >= 0; i-- {
			b.WriteString(alphabet[rnd.Intn(len(alphabet))])
		}
		return b.String()
	}

	for range 10000 {
		a, b := name(), name()
		s.Equal(strings.EqualFold(a, b), foldKey(a) == foldKey(b),
			"foldKey(%q) == foldKey(%q)", a, b)
	}
}

// TestFoldKeyCoversEveryOrbit walks the runes to make sure no orbit is missed,
// and that no two runes outside the same orbit collide on a key.
//
// Unprintable runes are skipped to keep the collision map small. Nothing is
// lost by it: every rune with a fold partner is a cased letter, and so is
// printable, while a rune that folds only to itself cannot split an orbit.
func (s *FoldSuite) TestFoldKeyCoversEveryOrbit() {
	seen := make(map[string]rune)
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			continue
		}

		key := foldKey(string(r))
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			s.Equal(key, foldKey(string(f)), "%U and %U fold together", r, f)
		}

		if other, ok := seen[key]; ok {
			s.True(strings.EqualFold(string(r), string(other)),
				"%U and %U collide on key %q", r, other, key)
			continue
		}
		seen[key] = r
	}
}

// TestFoldKeyDoesNotAllocateForCanonicalNames guards the fast path: section
// names are all-lowercase ASCII in practice, and those are already canonical.
func TestFoldKeyDoesNotAllocateForCanonicalNames(t *testing.T) { //nolint:paralleltest // testing.AllocsPerRun forbids parallel
	allocs := testing.AllocsPerRun(100, func() {
		foldKey("submodule")
	})
	assert.Zero(t, allocs)
}
