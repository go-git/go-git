package url

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type URLSuite struct{}

var _ = Suite(&URLSuite{})

func (s *URLSuite) TestMatchesScpLike(c *C) {
	examples := []string{
		"git@github.com:james/bond",
		"git@github.com:007/bond",
		"git@github.com:22:james/bond",
		"git@github.com:22:007/bond",
	}

	for _, url := range examples {
		c.Check(MatchesScpLike(url), Equals, true)
	}

	for _, url := range examples {
		c.Check(MatchesScpLikeExtended(url), Equals, true)
	}

	url := "lol@localhost:bond"
	c.Check(MatchesScpLike(url), Equals, false)
	c.Check(MatchesScpLikeExtended(url), Equals, true)
}

func (s *URLSuite) TestFindScpLikeComponents(c *C) {
	url := "git@github.com:james/bond"
	user, host, port, path := FindScpLikeComponents(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github.com")
	c.Check(port, Equals, "")
	c.Check(path, Equals, "james/bond")

	url = "git@github.com:007/bond"
	user, host, port, path = FindScpLikeComponents(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github.com")
	c.Check(port, Equals, "")
	c.Check(path, Equals, "007/bond")

	url = "git@github.com:22:james/bond"
	user, host, port, path = FindScpLikeComponents(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github.com")
	c.Check(port, Equals, "22")
	c.Check(path, Equals, "james/bond")

	url = "git@github.com:22:007/bond"
	user, host, port, path = FindScpLikeComponents(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github.com")
	c.Check(port, Equals, "22")
	c.Check(path, Equals, "007/bond")
}

// new tests

func (s *URLSuite) TestFindScpLikeComponentsNew(c *C) {
	url := "git@github.com:james/bond"
	user, host, port, path := FindScpLikeComponentsExtended(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github.com")
	c.Check(port, Equals, "")
	c.Check(path, Equals, "james/bond")

	url = "git@github.com:007/bond"
	user, host, port, path = FindScpLikeComponentsExtended(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github.com")
	c.Check(port, Equals, "")
	c.Check(path, Equals, "007/bond")

	url = "git@github.com:22:james/bond"
	user, host, port, path = FindScpLikeComponentsExtended(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github.com")
	c.Check(port, Equals, "22")
	c.Check(path, Equals, "james/bond")

	url = "git@github.com:22:007/bond"
	user, host, port, path = FindScpLikeComponentsExtended(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github.com")
	c.Check(port, Equals, "22")
	c.Check(path, Equals, "007/bond")

	url = "git@github:/bond"
	user, host, port, path = FindScpLikeComponentsExtended(url)

	c.Check(user, Equals, "git")
	c.Check(host, Equals, "github")
	c.Check(port, Equals, "")
	c.Check(path, Equals, "/bond")

	// the most important tests

	url = "lol@localhost:bond"
	user, host, port, path = FindScpLikeComponentsExtended(url)

	c.Check(user, Equals, "lol")
	c.Check(host, Equals, "localhost")
	c.Check(port, Equals, "")
	c.Check(path, Equals, "bond")

	url = "lol@localhost:22:bond"
	user, host, port, path = FindScpLikeComponentsExtended(url)

	c.Check(user, Equals, "lol")
	c.Check(host, Equals, "localhost")
	c.Check(port, Equals, "22")
	c.Check(path, Equals, "bond")
}
