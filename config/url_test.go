package config

import (
	. "gopkg.in/check.v1"
)

type URLSuite struct{}

var _ = Suite(&URLSuite{})

func (b *URLSuite) TestValidateInsteadOf(c *C) {
	goodURL := URL{
		Name:      "ssh://github.com",
		InsteadOf: "http://github.com",
	}
	badURL := URL{}
	c.Assert(goodURL.Validate(), IsNil)
	c.Assert(badURL.Validate(), NotNil)
}

func (b *URLSuite) TestMarshal(c *C) {
	expected := []byte(`[core]
	bare = false
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
`)

	cfg := NewConfig()
	cfg.URLs["ssh://git@github.com/"] = &URL{
		Name:      "ssh://git@github.com/",
		InsteadOf: "https://github.com/",
	}

	actual, err := cfg.Marshal()
	c.Assert(err, IsNil)
	c.Assert(string(actual), Equals, string(expected))
}

func (b *URLSuite) TestUnmarshal(c *C) {
	input := []byte(`[core]
	bare = false
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	c.Assert(err, IsNil)
	url := cfg.URLs["ssh://git@github.com/"]
	c.Assert(url.Name, Equals, "ssh://git@github.com/")
	c.Assert(url.InsteadOf, Equals, "https://github.com/")
}

func (b *URLSuite) TestApplyInsteadOf(c *C) {
	urlRule := URL{
		Name:      "ssh://github.com",
		InsteadOf: "http://github.com",
	}

	c.Assert(urlRule.ApplyInsteadOf("http://google.com"), Equals, "http://google.com")
	c.Assert(urlRule.ApplyInsteadOf("http://github.com/myrepo"), Equals, "ssh://github.com/myrepo")
}
