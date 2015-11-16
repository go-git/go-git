package git

import (
	"io"
	"os"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v2/clients/common"
	"gopkg.in/src-d/go-git.v2/core"
	"gopkg.in/src-d/go-git.v2/formats/packfile"
)

func Test(t *testing.T) { TestingT(t) }

type MockGitUploadPackService struct {
	Auth common.AuthMethod
}

func (s *MockGitUploadPackService) Connect(url common.Endpoint) error {
	return nil
}

func (s *MockGitUploadPackService) ConnectWithAuth(url common.Endpoint, auth common.AuthMethod) error {
	s.Auth = auth
	return nil
}

func (s *MockGitUploadPackService) Info() (*common.GitUploadPackInfo, error) {
	hash := core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	cap := common.NewCapabilities()
	cap.Decode("6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEADmulti_ack thin-pack side-band side-band-64k ofs-delta shallow no-progress include-tag multi_ack_detailed no-done symref=HEAD:refs/heads/master agent=git/2:2.4.8~dbussink-fix-enterprise-tokens-compilation-1167-gc7006cf")

	return &common.GitUploadPackInfo{
		Capabilities: cap,
		Head:         hash,
		Refs:         map[string]core.Hash{"refs/heads/master": hash},
	}, nil
}

func (s *MockGitUploadPackService) Fetch(*common.GitUploadPackRequest) (io.ReadCloser, error) {
	r, _ := os.Open("formats/packfile/fixtures/git-fixture.ref-delta")
	return r, nil
}

type SuiteCommon struct {
	repos map[string]*Repository
}

var _ = Suite(&SuiteCommon{})

var fixtureRepos = [...]struct {
	url      string
	packfile string
}{
	{"https://github.com/tyba/git-fixture.git", "formats/packfile/fixtures/git-fixture.ofs-delta"},
}

// create the repositories of the fixtures
func (s *SuiteCommon) SetUpSuite(c *C) {
	s.repos = make(map[string]*Repository, 0)
	for _, fixRepo := range fixtureRepos {
		s.repos[fixRepo.url] = NewPlainRepository()

		d, err := os.Open(fixRepo.packfile)
		defer func() {
			c.Assert(d.Close(), IsNil)
		}()
		c.Assert(err, IsNil)

		r := packfile.NewReader(d)
		r.Format = packfile.OFSDeltaFormat // TODO: how to know the format of a pack file ahead of time?

		_, err = r.Read(s.repos[fixRepo.url].Storage)
		c.Assert(err, IsNil)
	}
}

var countLinesTests = [...]struct {
	i string // the string we want to count lines from
	e int    // the expected number of lines in i
}{
	{"", 0},
	{"a", 1},
	{"a\n", 1},
	{"a\nb", 2},
	{"a\nb\n", 2},
	{"a\nb\nc", 3},
	{"a\nb\nc\n", 3},
	{"first line\n\tsecond line\nthird line\n", 3},
}

func (s *SuiteCommon) TestCountLines(c *C) {
	for i, t := range countLinesTests {
		o := CountLines(t.i)
		c.Assert(o, Equals, t.e, Commentf("subtest %d, input=%q", i, t.i))
	}
}

var findFileTests = []struct {
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

func (s *SuiteCommon) TestFindFile(c *C) {
	for i, t := range findFileTests {
		commit, err := s.repos[t.repo].Commit(core.NewHash(t.commit))
		c.Assert(err, IsNil, Commentf("subtest %d: %v (%s)", i, err, t.commit))

		file, found := FindFile(t.path, commit)
		c.Assert(found, Equals, t.found, Commentf("subtest %d, path=%s, commit=%s", i, t.path, t.commit))
		if found {
			c.Assert(file.Hash.String(), Equals, t.blobHash, Commentf("subtest %d, commit=%s, path=%s", i, t.commit, t.path))
		}
	}
}
