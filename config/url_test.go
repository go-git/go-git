package config

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type URLSuite struct {
	suite.Suite
}

func TestURLSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(URLSuite))
}

func (b *URLSuite) TestValidateInsteadOf() {
	goodURL := URL{
		Name:       "ssh://github.com",
		InsteadOfs: []string{"http://github.com"},
	}
	goodPushURL := URL{
		Name:           "ssh://github.com",
		PushInsteadOfs: []string{"http://github.com"},
	}
	badURL := URL{}
	b.Nil(goodURL.Validate())
	b.Nil(goodPushURL.Validate())
	b.NotNil(badURL.Validate())
}

func (b *URLSuite) TestMarshal() {
	expected := []byte(`[core]
	bare = false
	filemode = true
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
	filemode = true
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

func (b *URLSuite) TestMarshalPushInsteadOf() {
	expected := []byte(`[core]
	bare = false
	filemode = true
[url "ssh://git@github.com/"]
	pushInsteadOf = https://github.com/
`)

	cfg := NewConfig()
	cfg.URLs["ssh://git@github.com/"] = &URL{
		Name:           "ssh://git@github.com/",
		PushInsteadOfs: []string{"https://github.com/"},
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

func (b *URLSuite) TestUnmarshalPushInsteadOf() {
	input := []byte(`[core]
	bare = false
[url "ssh://git@github.com/"]
	pushInsteadOf = https://github.com/
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	b.NoError(err)
	url := cfg.URLs["ssh://git@github.com/"]
	b.Equal("ssh://git@github.com/", url.Name)
	b.Equal("https://github.com/", url.PushInsteadOfs[0])
	b.Equal("ssh://git@github.com/foobar", url.ApplyPushInsteadOf("https://github.com/foobar"))
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

func (b *URLSuite) TestApplyInsteadOfUsesLongestMatch() {
	urlRule := URL{
		Name:       "ssh://github.com/",
		InsteadOfs: []string{"https://example.com/", "https://example.com/team/"},
	}

	b.Equal("ssh://github.com/repo.git", urlRule.ApplyInsteadOf("https://example.com/team/repo.git"))
}

func (b *URLSuite) TestApplyPushInsteadOfUsesLongestMatch() {
	urlRule := URL{
		Name:           "ssh://github.com/",
		PushInsteadOfs: []string{"https://example.com/", "https://example.com/team/"},
	}

	b.Equal("ssh://github.com/repo.git", urlRule.ApplyPushInsteadOf("https://example.com/team/repo.git"))
}

func (b *URLSuite) TestRewriteLongestURLMatch() {
	urlRules := map[string]*URL{
		"ssh://github.com": {
			Name:       "ssh://github.com",
			InsteadOfs: []string{"http://github.com"},
		},
		"ssh://somethingelse.com": {
			Name:       "ssh://somethingelse.com",
			InsteadOfs: []string{"http://github.com/foobar"},
		},
	}

	rewritten, matched := rewriteLongestURLMatch("http://github.com/foobar/bingbash.git", urlRules, func(u *URL) []string {
		return u.InsteadOfs
	})

	b.True(matched)
	b.Equal("ssh://somethingelse.com/bingbash.git", rewritten)
}
