package config

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
)

type DecoderSuite struct {
	suite.Suite
}

func TestDecoderSuite(t *testing.T) {
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

func FuzzDecoder(f *testing.F) {
	f.Fuzz(func(_ *testing.T, input []byte) {
		d := NewDecoder(bytes.NewReader(input))
		cfg := &Config{}
		d.Decode(cfg)
	})
}
