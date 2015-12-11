package git

import (
	"os"

	"gopkg.in/src-d/go-git.v2/core"
	"gopkg.in/src-d/go-git.v2/formats/packfile"

	. "gopkg.in/check.v1"
)

type SuiteCommit struct {
	repos map[string]*Repository
}

var _ = Suite(&SuiteCommit{})

// create the repositories of the fixtures
func (s *SuiteCommit) SetUpSuite(c *C) {
	fixtureRepos := [...]struct {
		url      string
		packfile string
	}{
		{"https://github.com/tyba/git-fixture.git", "formats/packfile/fixtures/git-fixture.ofs-delta"},
	}
	s.repos = make(map[string]*Repository, 0)
	for _, fixRepo := range fixtureRepos {
		s.repos[fixRepo.url] = NewPlainRepository()

		d, err := os.Open(fixRepo.packfile)
		c.Assert(err, IsNil)

		r := packfile.NewReader(d)
		r.Format = packfile.OFSDeltaFormat // TODO: how to know the format of a pack file ahead of time?

		_, err = r.Read(s.repos[fixRepo.url].Storage)
		c.Assert(err, IsNil)

		c.Assert(d.Close(), IsNil)
	}
}

func (s *SuiteCommit) TestIterClose(c *C) {
	i := &iter{ch: make(chan core.Object, 1)}
	i.Close()
	i.Close()
}

var fileTests = []struct {
	repo     string // the repo name as in localRepos
	commit   string // the commit to search for the file
	path     string // the path of the file to find
	blobHash string // expected hash of the returned file
	found    bool   // expected found value
}{
	// use git ls-tree commit to get the hash of the blobs
	{"https://github.com/tyba/git-fixture.git", "b029517f6300c2da0f4b651b8642506cd6aaf45d", "not-found",
		"", false},
	{"https://github.com/tyba/git-fixture.git", "b029517f6300c2da0f4b651b8642506cd6aaf45d", ".gitignore",
		"32858aad3c383ed1ff0a0f9bdf231d54a00c9e88", true},
	{"https://github.com/tyba/git-fixture.git", "b029517f6300c2da0f4b651b8642506cd6aaf45d", "LICENSE",
		"c192bd6a24ea1ab01d78686e417c8bdc7c3d197f", true},

	{"https://github.com/tyba/git-fixture.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "not-found",
		"", false},
	{"https://github.com/tyba/git-fixture.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", ".gitignore",
		"32858aad3c383ed1ff0a0f9bdf231d54a00c9e88", true},
	{"https://github.com/tyba/git-fixture.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "binary.jpg",
		"d5c0f4ab811897cadf03aec358ae60d21f91c50d", true},
	{"https://github.com/tyba/git-fixture.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "LICENSE",
		"c192bd6a24ea1ab01d78686e417c8bdc7c3d197f", true},

	{"https://github.com/tyba/git-fixture.git", "35e85108805c84807bc66a02d91535e1e24b38b9", "binary.jpg",
		"d5c0f4ab811897cadf03aec358ae60d21f91c50d", true},
	{"https://github.com/tyba/git-fixture.git", "b029517f6300c2da0f4b651b8642506cd6aaf45d", "binary.jpg",
		"", false},

	{"https://github.com/tyba/git-fixture.git", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5", "CHANGELOG",
		"d3ff53e0564a9f87d8e84b6e28e5060e517008aa", true},
	{"https://github.com/tyba/git-fixture.git", "1669dce138d9b841a518c64b10914d88f5e488ea", "CHANGELOG",
		"d3ff53e0564a9f87d8e84b6e28e5060e517008aa", true},
	{"https://github.com/tyba/git-fixture.git", "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69", "CHANGELOG",
		"d3ff53e0564a9f87d8e84b6e28e5060e517008aa", true},
	{"https://github.com/tyba/git-fixture.git", "35e85108805c84807bc66a02d91535e1e24b38b9", "CHANGELOG",
		"d3ff53e0564a9f87d8e84b6e28e5060e517008aa", false},
	{"https://github.com/tyba/git-fixture.git", "b8e471f58bcbca63b07bda20e428190409c2db47", "CHANGELOG",
		"d3ff53e0564a9f87d8e84b6e28e5060e517008aa", true},
	{"https://github.com/tyba/git-fixture.git", "b029517f6300c2da0f4b651b8642506cd6aaf45d", "CHANGELOG",
		"d3ff53e0564a9f87d8e84b6e28e5060e517008aa", false},
}

func (s *SuiteCommit) TestFile(c *C) {
	for i, t := range fileTests {
		commit, err := s.repos[t.repo].Commit(core.NewHash(t.commit))
		c.Assert(err, IsNil, Commentf("subtest %d: %v (%s)", i, err, t.commit))

		file, err := commit.File(t.path)
		found := err == nil
		c.Assert(found, Equals, t.found, Commentf("subtest %d, path=%s, commit=%s", i, t.path, t.commit))
		if found {
			c.Assert(file.Hash.String(), Equals, t.blobHash, Commentf("subtest %d, commit=%s, path=%s", i, t.commit, t.path))
		}
	}
}
