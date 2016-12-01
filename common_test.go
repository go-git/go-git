package git

import (
	"fmt"
	"io"
	"os"
	"testing"

	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packfile"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp"
	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/client"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type BaseSuite struct {
	fixtures.Suite

	Repository   *Repository
	Repositories map[string]*Repository
}

func (s *BaseSuite) SetUpSuite(c *C) {
	s.Suite.SetUpSuite(c)
	s.installMockProtocol(c)
	s.buildRepository(c)

	s.Repositories = make(map[string]*Repository, 0)
	s.buildRepositories(c, fixtures.Basic().ByTag("packfile"))
}

func (s *BaseSuite) installMockProtocol(c *C) {
	client.InstallProtocol("https", &MockClient{})
}

func (s *BaseSuite) buildRepository(c *C) {
	f := fixtures.Basic().One()

	var err error
	s.Repository, err = NewFilesystemRepository(f.DotGit().Base())
	c.Assert(err, IsNil)
}

func (s *BaseSuite) buildRepositories(c *C, f fixtures.Fixtures) {
	for _, fixture := range f {
		r := NewMemoryRepository()

		f := fixture.Packfile()
		defer f.Close()

		n := packfile.NewScanner(f)
		d, err := packfile.NewDecoder(n, r.s)
		c.Assert(err, IsNil)
		_, err = d.Decode()
		c.Assert(err, IsNil)

		s.Repositories[fixture.URL] = r
	}
}

const RepositoryFixture = "https://github.com/git-fixtures/basic.git"

type MockClient struct{}

type MockFetchPackSession struct {
	endpoint transport.Endpoint
	auth     transport.AuthMethod
}

func (c *MockClient) NewFetchPackSession(ep transport.Endpoint) (
	transport.FetchPackSession, error) {

	return &MockFetchPackSession{
		endpoint: ep,
		auth:     nil,
	}, nil
}

func (c *MockClient) NewSendPackSession(ep transport.Endpoint) (
	transport.SendPackSession, error) {

	return nil, fmt.Errorf("not supported")
}

func (c *MockFetchPackSession) SetAuth(auth transport.AuthMethod) error {
	c.auth = auth
	return nil
}

func (c *MockFetchPackSession) AdvertisedReferences() (*packp.AdvRefs, error) {

	h := fixtures.ByURL(c.endpoint.String()).One().Head

	cap := capability.NewList()
	cap.Set(capability.Agent, "go-git/tests")

	ref := plumbing.ReferenceName("refs/heads/master")
	branch := plumbing.ReferenceName("refs/heads/branch")
	tag := plumbing.ReferenceName("refs/tags/v1.0.0")

	a := packp.NewAdvRefs()
	a.Capabilities = cap
	a.Head = &h
	a.AddReference(plumbing.NewSymbolicReference(plumbing.HEAD, ref))
	a.AddReference(plumbing.NewHashReference(ref, h))
	a.AddReference(plumbing.NewHashReference(tag, h))
	a.AddReference(plumbing.NewHashReference(branch, plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881")))

	return a, nil
}

func (c *MockFetchPackSession) FetchPack(
	r *packp.UploadPackRequest) (io.ReadCloser, error) {

	if !r.Capabilities.Supports(capability.Agent) {
		return nil, fmt.Errorf("" +
			"invalid test rquest, missing Agent capability, the request" +
			"should be created using NewUploadPackRequestFromCapabilities",
		)
	}

	f := fixtures.ByURL(c.endpoint.String())

	if len(r.Wants) == 1 {
		return f.Exclude("single-branch").One().Packfile(), nil
	}

	return f.One().Packfile(), nil
}

func (c *MockFetchPackSession) Close() error {
	return nil
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

			repos[fixture.url] = NewMemoryRepository()

			f, err := os.Open(fixture.packfile)
			c.Assert(err, IsNil, comment)

			r := packfile.NewScanner(f)
			d, err := packfile.NewDecoder(r, repos[fixture.url].s)
			c.Assert(err, IsNil, comment)
			_, err = d.Decode()
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

func (s *BaseSuite) Clone(url string) *Repository {
	r := NewMemoryRepository()
	if err := r.Clone(&CloneOptions{URL: url}); err != nil {
		panic(err)
	}

	return r
}

func (s *BaseSuite) NewRepository(f *fixtures.Fixture) *Repository {
	storage, err := filesystem.NewStorage(f.DotGit())
	if err != nil {
		panic(err)
	}

	r, err := NewRepository(storage)
	if err != nil {
		panic(err)
	}

	return r
}
