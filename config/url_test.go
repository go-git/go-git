package config

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type URLSuite struct {
	suite.Suite
}

func TestURLSuite(t *testing.T) {
	suite.Run(t, new(URLSuite))
}

func (b *URLSuite) TestValidateInsteadOf() {
	goodURL := URL{
		Name:       "ssh://github.com",
		InsteadOfs: []string{"http://github.com"},
	}
	badURL := URL{}
	b.Nil(goodURL.Validate())
	b.NotNil(badURL.Validate())
}

func (b *URLSuite) TestMarshal() {
	expected := []byte(`[core]
	bare = false
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
`)

	cfg := NewConfig()
	cfg.URLs["ssh://git@github.com/"] = &URL{
		Name:       "ssh://git@github.com/",
		InsteadOfs: []string{"https://github.com/"},
	}

	actual, err := cfg.Marshal()
	b.Nil(err)
	b.Equal(string(expected), string(actual))
}

func (b *URLSuite) TestMarshalMultipleInsteadOf() {
	expected := []byte(`[core]
	bare = false
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
	insteadOf = https://google.com/
`)

	cfg := NewConfig()
	cfg.URLs["ssh://git@github.com/"] = &URL{
		Name:       "ssh://git@github.com/",
		InsteadOfs: []string{"https://github.com/", "https://google.com/"},
	}

	actual, err := cfg.Marshal()
	b.NoError(err)
	b.Equal(string(expected), string(actual))
}

func (b *URLSuite) TestUnmarshal() {
	input := []byte(`[core]
	bare = false
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	b.NoError(err)
	url := cfg.URLs["ssh://git@github.com/"]
	b.Equal("ssh://git@github.com/", url.Name)
	b.Equal("https://github.com/", url.InsteadOfs[0])
}

func (b *URLSuite) TestUnmarshalMultipleInsteadOf() {
	input := []byte(`[core]
	bare = false
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
	insteadOf = https://google.com/
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	b.Nil(err)
	url := cfg.URLs["ssh://git@github.com/"]
	b.Equal("ssh://git@github.com/", url.Name)

	b.Equal("ssh://git@github.com/foobar", url.ApplyInsteadOf("https://github.com/foobar"))
	b.Equal("ssh://git@github.com/foobar", url.ApplyInsteadOf("https://google.com/foobar"))
}

func (b *URLSuite) TestUnmarshalDuplicateUrls() {
	input := []byte(`[core]
	bare = false
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
[url "ssh://git@github.com/"]
	insteadOf = https://google.com/
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	b.Nil(err)
	url := cfg.URLs["ssh://git@github.com/"]
	b.Equal("ssh://git@github.com/", url.Name)

	b.Equal("ssh://git@github.com/foobar", url.ApplyInsteadOf("https://github.com/foobar"))
	b.Equal("ssh://git@github.com/foobar", url.ApplyInsteadOf("https://google.com/foobar"))
}

func (b *URLSuite) TestApplyInsteadOf() {
	urlRule := URL{
		Name:       "ssh://github.com",
		InsteadOfs: []string{"http://github.com"},
	}

	b.Equal("http://google.com", urlRule.ApplyInsteadOf("http://google.com"))
	b.Equal("ssh://github.com/myrepo", urlRule.ApplyInsteadOf("http://github.com/myrepo"))
}

func (b *URLSuite) TestFindLongestInsteadOfMatch() {
	urlRules := map[string]*URL{
		"ssh://github.com": &URL{
			Name:       "ssh://github.com",
			InsteadOfs: []string{"http://github.com"},
		},
		"ssh://somethingelse.com": &URL{
			Name:       "ssh://somethingelse.com",
			InsteadOfs: []string{"http://github.com/foobar"},
		},
	}

	longestUrl := findLongestInsteadOfMatch("http://github.com/foobar/bingbash.git", urlRules)

	b.Equal("ssh://somethingelse.com", longestUrl.Name)
}
