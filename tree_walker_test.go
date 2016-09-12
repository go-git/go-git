package git

import (
	"io"

	"gopkg.in/src-d/go-git.v4/core"

	. "gopkg.in/check.v1"
)

type TreeWalkerSuite struct {
	BaseSuite
}

var _ = Suite(&TreeWalkerSuite{})

func (s *TreeWalkerSuite) TestNext(c *C) {
	r := s.Repository
	commit, err := r.Commit(core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	c.Assert(err, IsNil)

	tree, err := commit.Tree()
	c.Assert(err, IsNil)

	walker := NewTreeWalker(r, tree)
	for _, e := range treeWalkerExpects {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}

		c.Assert(err, IsNil)
		c.Assert(name, Equals, e.Path)
		c.Assert(entry.Name, Equals, e.Name)
		c.Assert(entry.Mode.String(), Equals, e.Mode)
		c.Assert(entry.Hash.String(), Equals, e.Hash)
	}
}

var treeWalkerExpects = []struct {
	Path, Mode, Name, Hash string
}{
	{Path: ".gitignore", Mode: "-rw-r--r--", Name: ".gitignore", Hash: "32858aad3c383ed1ff0a0f9bdf231d54a00c9e88"},
	{Path: "CHANGELOG", Mode: "-rw-r--r--", Name: "CHANGELOG", Hash: "d3ff53e0564a9f87d8e84b6e28e5060e517008aa"},
	{Path: "LICENSE", Mode: "-rw-r--r--", Name: "LICENSE", Hash: "c192bd6a24ea1ab01d78686e417c8bdc7c3d197f"},
	{Path: "binary.jpg", Mode: "-rw-r--r--", Name: "binary.jpg", Hash: "d5c0f4ab811897cadf03aec358ae60d21f91c50d"},
	{Path: "go", Mode: "d---------", Name: "go", Hash: "a39771a7651f97faf5c72e08224d857fc35133db"},
	{Path: "go/example.go", Mode: "-rw-r--r--", Name: "example.go", Hash: "880cd14280f4b9b6ed3986d6671f907d7cc2a198"},
	{Path: "json", Mode: "d---------", Name: "json", Hash: "5a877e6a906a2743ad6e45d99c1793642aaf8eda"},
	{Path: "json/long.json", Mode: "-rw-r--r--", Name: "long.json", Hash: "49c6bb89b17060d7b4deacb7b338fcc6ea2352a9"},
	{Path: "json/short.json", Mode: "-rw-r--r--", Name: "short.json", Hash: "c8f1d8c61f9da76f4cb49fd86322b6e685dba956"},
	{Path: "php", Mode: "d---------", Name: "php", Hash: "586af567d0bb5e771e49bdd9434f5e0fb76d25fa"},
	{Path: "php/crappy.php", Mode: "-rw-r--r--", Name: "crappy.php", Hash: "9a48f23120e880dfbe41f7c9b7b708e9ee62a492"},
	{Path: "vendor", Mode: "d---------", Name: "vendor", Hash: "cf4aa3b38974fb7d81f367c0830f7d78d65ab86b"},
	{Path: "vendor/foo.go", Mode: "-rw-r--r--", Name: "foo.go", Hash: "9dea2395f5403188298c1dabe8bdafe562c491e3"},
}
