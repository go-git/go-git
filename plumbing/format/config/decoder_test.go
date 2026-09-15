package config

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/go-git/gcfg/v2"
	"github.com/stretchr/testify/suite"
)

type DecoderSuite struct {
	suite.Suite
}

func TestDecoderSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(DecoderSuite))
}

func (s *DecoderSuite) TestDecode() {
	for idx, fixture := range fixtures {
		r := bytes.NewReader([]byte(fixture.Raw))
		d := NewDecoder(r)
		cfg := &Config{}
		err := d.Decode(cfg)
		s.NoError(err, fmt.Sprintf("decoder error for fixture: %d", idx))
		buf := bytes.NewBuffer(nil)
		e := NewEncoder(buf)
		_ = e.Encode(cfg)
		s.Equal(fixture.Config, cfg, fmt.Sprintf("bad result for fixture: %d, %s", idx, buf.String()))
	}
}

func (s *DecoderSuite) TestDecodeFailsWithNilConfig() {
	err := NewDecoder(strings.NewReader("[section]\n\tkey = value\n")).Decode(nil)
	s.ErrorContains(err, "config is nil")
}

func (s *DecoderSuite) TestDecodeFailsWithIdentBeforeSection() {
	t := `
	key=value
	[section]
	key=value
	`
	decodeFails(s, t)
}

func (s *DecoderSuite) TestDecodeFailsWithEmptySectionName() {
	t := `
	[]
	key=value
	`
	decodeFails(s, t)
}

func (s *DecoderSuite) TestDecodeSucceedsWithEmptySubsectionName() {
	t := `
	[remote ""]
	key=value
	`
	decodeSucceeds(s, t)
}

func (s *DecoderSuite) TestDecodeFailsWithBadSubsectionName() {
	t := `
	[remote origin"]
	key=value
	`
	decodeFails(s, t)
	t = `
	[remote "origin]
	key=value
	`
	decodeFails(s, t)
}

func (s *DecoderSuite) TestDecodeFailsWithTrailingGarbage() {
	t := `
	[remote]garbage
	key=value
	`
	decodeFails(s, t)
	t = `
	[remote "origin"]garbage
	key=value
	`
	decodeFails(s, t)
}

func (s *DecoderSuite) TestDecodeFailsWithGarbage() {
	decodeFails(s, "---")
	decodeFails(s, "????")
	decodeFails(s, "[sect\nkey=value")
	decodeFails(s, "sect]\nkey=value")
	decodeFails(s, `[section]key="value`)
	decodeFails(s, `[section]key=value"`)
}

// referenceDecode decodes the way Decode did before it kept an index, by
// going through the public Config.Section, Section.Subsection and
// Config.AddOption lookups.
func referenceDecode(config *Config, in string) error {
	cb := func(s, ss, k, v string, _ bool) error {
		if ss == "" && k == "" {
			config.Section(s)
			return nil
		}

		if ss != "" && k == "" {
			config.Section(s).Subsection(ss)
			return nil
		}

		config.AddOption(s, ss, k, v)
		return nil
	}
	return gcfg.ReadWithCallback(strings.NewReader(in), cb)
}

// TestDecodeMatchesPublicLookups pins the lookup semantics of the decoder
// index to those of Config.Section and Section.Subsection, which it
// reimplements: section names match case insensitively and the last section of
// a name wins, while subsection names match exactly.
//
// The reference is the code Decode used before it kept an index, so this is
// what catches the two implementations drifting apart, for instance if
// Section.IsName or Subsection.IsName changes which names it considers equal.
func (s *DecoderSuite) TestDecodeMatchesPublicLookups() {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"case insensitive section name", "[core]\na = 1\n[CORE]\nb = 2\n[Core]\nc = 3\n"},
		{"last section of a name wins", "[a]\nx = 1\n[b]\ny = 2\n[a]\nz = 3\n[A]\nw = 4\n"},
		{"case sensitive subsection name", "[core \"x\"]\na = 1\n[core \"X\"]\nb = 2\n[CORE \"x\"]\nc = 3\n"},
		{"long s folds to s", "[\u017f]\na = 1\n[s]\nb = 2\n[S]\nc = 3\n"},
		{"kelvin sign folds to k", "[\u212a]\na = 1\n[k]\nb = 2\n[K]\nc = 3\n"},
		{"sharp s does not fold to ss", "[\u00df]\na = 1\n[ss]\nb = 2\n[\u1e9e]\nc = 3\n"},
		{"empty subsection name is no subsection", "[remote \"\"]\na = 1\n[remote]\nb = 2\n"},
		{"section reopened after a subsection", "[core]\n[core \"x\"]\n[core]\na = 1\n"},
		{"subsection reopened after a section", "[remote \"origin\"]\nurl = u\n[core]\nbare = true\n[remote \"origin\"]\nfetch = f\n"},
	} {
		s.Run(tc.name, func() {
			expected := New()
			s.Require().NoError(referenceDecode(expected, tc.in))

			obtained := New()
			s.Require().NoError(NewDecoder(strings.NewReader(tc.in)).Decode(obtained))

			s.Equal(expected, obtained)
		})
	}
}

// TestDecodeFoldsSectionNamesIntoOne spells out the answer the equivalence in
// TestDecodeMatchesPublicLookups only compares against, so that tidying the
// reference cannot quietly take the guard with it.
func (s *DecoderSuite) TestDecodeFoldsSectionNamesIntoOne() {
	in := "[\u017f]\na = 1\n[s]\nb = 2\n[S \"sub\"]\nc = 3\n"

	obtained := New()
	s.Require().NoError(NewDecoder(strings.NewReader(in)).Decode(obtained))

	s.Equal(&Config{Sections: Sections{
		{
			Name:    "\u017f",
			Options: Options{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}},
			Subsections: Subsections{
				{Name: "sub", Options: Options{{Key: "c", Value: "3"}}},
			},
		},
	}}, obtained)
}

// TestDecodeIntoNonEmptyConfig covers decoding into a config that already
// holds sections, which the index has to pick up to keep resolving names the
// way Config.Section does.
func (s *DecoderSuite) TestDecodeIntoNonEmptyConfig() {
	seed := func() *Config {
		return &Config{Sections: Sections{
			{Name: "core", Options: Options{{Key: "bare", Value: "true"}}},
			{Name: "CORE"},
			{Name: "remote", Subsections: Subsections{
				{Name: "Origin"}, {Name: "origin"}, {Name: "origin"},
			}},
		}}
	}

	in := "[Core]\na = 1\n[remote \"origin\"]\nurl = u\n[remote \"ORIGIN\"]\nurl = v\n"

	expected := seed()
	s.Require().NoError(referenceDecode(expected, in))

	obtained := seed()
	s.Require().NoError(NewDecoder(strings.NewReader(in)).Decode(obtained))

	s.Equal(expected, obtained)
}

// TestDecodeScalesLinearly guards the lookups the decoder performs for every
// header and every option line against degrading into a scan of everything
// decoded so far. Without an index each of these shapes costs
// O(lines × sections × len(name)), which is enough for a config of a few
// hundred kilobytes from an untrusted source, a .gitmodules file for
// instance, to take tens of seconds to decode.
//
// The assertion is on how the cost grows with the input rather than on an
// absolute duration, which is far more robust on a loaded machine than a
// threshold would be, though not immune to one. If it ever does flake, shrink
// size: the separation between the two regimes does not depend on it, and a
// smaller decode is both quicker and less exposed to a scheduling hiccup.
// Raising tolerance is not the lever it looks like: at 12 the bound would be
// 192, and the broken many subsections shape has been measured coming in under
// that.
//
// When the index is broken this takes a couple of seconds to fail, against the
// few tens of milliseconds it costs when it is not.
func (s *DecoderSuite) TestDecodeScalesLinearly() {
	const (
		size   = 4 * 1024
		factor = 16
		// Growing the input by factor may cost factor times as much,
		// with room to spare for measurement noise. The quadratic
		// growth this guards against costs factor² times as much.
		tolerance = 6
		// Decoding is timed more than once, keeping the fastest run, to
		// take the edge off a scheduling hiccup.
		runs = 3
	)

	decode := func(in string) time.Duration {
		best := time.Duration(math.MaxInt64)
		for range runs {
			start := time.Now()
			err := NewDecoder(strings.NewReader(in)).Decode(New())
			elapsed := time.Since(start)

			s.Require().NoError(err)
			best = min(best, elapsed)
		}
		return best
	}

	for _, shape := range []struct {
		name string
		gen  func(size int) string
	}{{
		// One long section name followed by short option lines, each
		// of which has to resolve that name. This is the shape of the
		// input OSS-Fuzz reported a timeout for.
		"options under a long section", func(size int) string {
			var b strings.Builder
			b.WriteByte('[')
			for b.Len() < size/2 {
				b.WriteString("\U00010490")
			}
			b.WriteString("]\n")
			for b.Len() < size {
				b.WriteString("a\n")
			}
			return b.String()
		},
	}, {
		"many sections", func(size int) string {
			var b strings.Builder
			for i := 0; b.Len() < size; i++ {
				fmt.Fprintf(&b, "[s%d]\n", i)
			}
			return b.String()
		},
	}, {
		"many subsections", func(size int) string {
			var b strings.Builder
			for i := 0; b.Len() < size; i++ {
				fmt.Fprintf(&b, "[s \"%d\"]\n", i)
			}
			return b.String()
		},
	}} {
		s.Run(shape.name, func() {
			base := decode(shape.gen(size))
			grown := decode(shape.gen(size * factor))

			s.T().Logf("%s for %d bytes, %s for %d bytes",
				base, size, grown, size*factor)
			s.Less(grown, base*factor*tolerance,
				"decoding %d times more input took more than %d times longer",
				factor, factor*tolerance)
		})
	}
}

func decodeFails(s *DecoderSuite, text string) {
	r := bytes.NewReader([]byte(text))
	d := NewDecoder(r)
	cfg := &Config{}
	err := d.Decode(cfg)
	s.NotNil(err)
}

func decodeSucceeds(s *DecoderSuite, text string) {
	r := bytes.NewReader([]byte(text))
	d := NewDecoder(r)
	cfg := &Config{}
	err := d.Decode(cfg)
	s.NoError(err)

	s.True(cfg.HasSection("remote"))
	remote := cfg.Section("remote")
	s.True(remote.HasOption("key"))
	s.Equal("value", remote.Option("key"))
}

func FuzzConfigDecoder(f *testing.F) {
	f.Fuzz(func(_ *testing.T, input []byte) {
		d := NewDecoder(bytes.NewReader(input))
		cfg := &Config{}
		d.Decode(cfg)
	})
}
