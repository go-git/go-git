package git

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"testing"

	"gopkg.in/src-d/go-git.v3/clients/common"
	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/formats/packfile"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type MockGitUploadPackService struct {
	Auth common.AuthMethod
	RC   io.ReadCloser
}

func (s *MockGitUploadPackService) Connect(url common.Endpoint) error {
	return nil
}

func (s *MockGitUploadPackService) ConnectWithAuth(url common.Endpoint, auth common.AuthMethod) error {
	s.Auth = auth
	return nil
}

func (s *MockGitUploadPackService) Info() (*common.GitUploadPackInfo, error) {
	h := core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	c := common.NewCapabilities()
	c.Decode("6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEADmulti_ack thin-pack side-band side-band-64k ofs-delta shallow no-progress include-tag multi_ack_detailed no-done symref=HEAD:refs/heads/master agent=git/2:2.4.8~dbussink-fix-enterprise-tokens-compilation-1167-gc7006cf")

	return &common.GitUploadPackInfo{
		Capabilities: c,
		Head:         h,
		Refs:         map[string]core.Hash{"refs/heads/master": h},
	}, nil
}

func (s *MockGitUploadPackService) Fetch(*common.GitUploadPackRequest) (io.ReadCloser, error) {
	var err error
	s.RC, err = os.Open("formats/packfile/fixtures/git-fixture.ref-delta")

	return s.RC, err
}

type packedFixture struct {
	url      string
	packfile string
}

var fixtureRepos = []packedFixture{
	{"https://github.com/tyba/git-fixture.git", "formats/packfile/fixtures/git-fixture.ofs-delta"},
	{"https://github.com/jamesob/desk.git", "formats/packfile/fixtures/jamesob-desk.pack"},
	{"https://github.com/spinnaker/spinnaker.git", "formats/packfile/fixtures/spinnaker-spinnaker.pack"},
}

func unpackFixtures(c *C, fixtures ...[]packedFixture) map[string]*Repository {
	repos := make(map[string]*Repository, 0)
	for _, group := range fixtures {
		for _, fixture := range group {
			if _, existing := repos[fixture.url]; existing {
				continue
			}

			comment := Commentf("fixture packfile: %q", fixture.packfile)

			repos[fixture.url] = NewPlainRepository()

			f, err := os.Open(fixture.packfile)
			c.Assert(err, IsNil, comment)

			// increase memory consumption to speed up tests
			data, err := ioutil.ReadAll(f)
			c.Assert(err, IsNil)
			memStream := bytes.NewReader(data)
			r := packfile.NewStream(memStream)

			d := packfile.NewDecoder(r)
			err = d.Decode(repos[fixture.url].Storage)
			c.Assert(err, IsNil, comment)

			c.Assert(f.Close(), IsNil, comment)
		}
	}

	return repos
}

type SuiteCommon struct{}

var _ = Suite(&SuiteCommon{})

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
	{"a\n\n\nb\n", 4},
	{"first line\n\tsecond line\nthird line\n", 3},
}

func (s *SuiteCommon) TestCountLines(c *C) {
	for i, t := range countLinesTests {
		o := countLines(t.i)
		c.Assert(o, Equals, t.e, Commentf("subtest %d, input=%q", i, t.i))
	}
}
