package git

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
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
	badURL := URL{}
	b.Nil(goodURL.Validate())
	b.NotNil(badURL.Validate())
}

func (b *URLSuite) TestMarshal() {
	expected := []byte(`[core]
	bare = false
	filemode = true
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
`)

	cfg := config.NewConfig()
	addURLConfig(cfg, &URL{
		Name:       "ssh://git@github.com/",
		InsteadOfs: []string{"https://github.com/"},
	})

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

	cfg := config.NewConfig()
	addURLConfig(cfg, &URL{
		Name:       "ssh://git@github.com/",
		InsteadOfs: []string{"https://github.com/", "https://google.com/"},
	})

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

	cfg := config.NewConfig()
	err := cfg.Unmarshal(input)
	b.NoError(err)
	b.Require().Len(urlConfigs(cfg), 1)
	url := urlConfigs(cfg)[0]
	b.NotNil(url)
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

	cfg := config.NewConfig()
	err := cfg.Unmarshal(input)
	b.Nil(err)
	b.Require().Len(urlConfigs(cfg), 1)
	url := urlConfigs(cfg)[0]
	b.NotNil(url)
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

	cfg := config.NewConfig()
	err := cfg.Unmarshal(input)
	b.Nil(err)
	b.Require().Len(urlConfigs(cfg), 1)
	url := urlConfigs(cfg)[0]
	b.NotNil(url)
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

func (b *URLSuite) TestApplyLongestInsteadOfMatch() {
	urlRules := []*URL{
		{
			Name:       "ssh://github.com",
			InsteadOfs: []string{"http://github.com"},
		},
		{
			Name:       "ssh://somethingelse.com",
			InsteadOfs: []string{"http://github.com/foobar"},
		},
	}

	rewrittenURL, matched := applyLongestInsteadOfMatch("http://github.com/foobar/bingbash.git", urlRules)

	b.True(matched, "Should find a match")
	b.Equal("ssh://somethingelse.com/bingbash.git", rewrittenURL)
}

func (b *URLSuite) TestApplyInsteadOfLongestMatchWithinSameURL() {
	// Test the edge case where a single URL has multiple insteadOf values
	// and both match the given URL - the longest should win
	url := &URL{
		Name: "ssh://git@github.com/",
		InsteadOfs: []string{
			"https://github.com/",
			"https://github.com/enterprise/",
		},
	}

	// Both insteadOf values match, but the longer one should be used
	result := url.ApplyInsteadOf("https://github.com/enterprise/user/repo.git")
	b.Equal("ssh://git@github.com/user/repo.git", result)

	// Also test with the shorter match
	result = url.ApplyInsteadOf("https://github.com/user/repo.git")
	b.Equal("ssh://git@github.com/user/repo.git", result)
}

func (b *URLSuite) TestApplyInsteadOfLongestMatchIntegration() {
	// Integration test: verify longest match within a URL's insteadOf list
	input := []byte(`[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
	insteadOf = https://github.com/enterprise/
[remote "origin"]
	url = https://github.com/enterprise/user/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`)

	cfg := config.NewConfig()
	err := cfg.Unmarshal(input)
	b.NoError(err)

	// The longer insteadOf should be used
	origin := remoteConfigs(cfg)["origin"]
	b.NotNil(origin)
	b.Equal([]string{"ssh://git@github.com/user/repo.git"}, origin.URLs)
}

func (b *URLSuite) TestApplyInsteadOfDuplicateInsteadOfValues() {
	// When multiple URLs have the same insteadOf value (same length),
	// use config file order (first wins), matching git's behavior.
	input := []byte(`[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
[url "git@github.com:"]
	insteadOf = https://github.com/
[remote "origin"]
	url = https://github.com/user/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`)

	cfg := config.NewConfig()
	err := cfg.Unmarshal(input)
	b.NoError(err)

	// First URL in config file should be used
	origin := remoteConfigs(cfg)["origin"]
	b.NotNil(origin)
	b.Equal([]string{"ssh://git@github.com/user/repo.git"}, origin.URLs)
}
