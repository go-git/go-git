package git

import (
	"io"
	"io/ioutil"
	"os"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
	"gopkg.in/src-d/go-git.v4/storage/memory"
	osfs "gopkg.in/src-d/go-git.v4/utils/fs/os"

	. "gopkg.in/check.v1"
)

const FixRefSpec = config.RefSpec("+refs/heads/*:refs/remotes/origin/*")

type RemoteSuite struct {
	BaseSuite
}

var _ = Suite(&RemoteSuite{})

func (s *RemoteSuite) TestConnect(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})

	err := r.Connect()
	c.Assert(err, IsNil)
}

func (s *RemoteSuite) TestnewRemoteInvalidEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux"})

	err := r.Connect()
	c.Assert(err, NotNil)
}

func (s *RemoteSuite) TestnewRemoteInvalidSchemaEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux://foo"})

	err := r.Connect()
	c.Assert(err, NotNil)
}

func (s *RemoteSuite) TestInfo(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Info(), IsNil)
	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Info(), NotNil)
	c.Assert(r.Info().Capabilities.Get("ofs-delta"), NotNil)
}

func (s *RemoteSuite) TestDefaultBranch(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Head().Name(), Equals, plumbing.ReferenceName("refs/heads/master"))
}

func (s *RemoteSuite) TestCapabilities(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Connect(), IsNil)
	c.Assert(r.Capabilities().Get("agent").Values, HasLen, 1)
}

func (s *RemoteSuite) TestFetch(c *C) {
	sto := memory.NewStorage()
	r := newRemote(sto, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Connect(), IsNil)

	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{FixRefSpec},
	})

	c.Assert(err, IsNil)
	c.Assert(sto.Objects, HasLen, 31)

	expectedRefs := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	}

	for _, exp := range expectedRefs {
		r, _ := sto.Reference(exp.Name())
		c.Assert(exp.String(), Equals, r.String())
	}
}

type mockPackfileWriter struct {
	Storer
	PackfileWriterCalled bool
}

func (m *mockPackfileWriter) PackfileWriter() (io.WriteCloser, error) {
	m.PackfileWriterCalled = true
	return m.Storer.(storer.PackfileWriter).PackfileWriter()
}

func (s *RemoteSuite) TestFetchWithPackfileWriter(c *C) {
	dir, err := ioutil.TempDir("", "fetch")
	c.Assert(err, IsNil)

	defer os.RemoveAll(dir) // clean up

	fss, err := filesystem.NewStorage(osfs.New(dir))
	c.Assert(err, IsNil)

	mock := &mockPackfileWriter{Storer: fss}

	r := newRemote(mock, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Connect(), IsNil)

	err = r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{FixRefSpec},
	})

	c.Assert(err, IsNil)

	var count int
	iter, err := mock.IterObjects(plumbing.AnyObject)
	c.Assert(err, IsNil)

	iter.ForEach(func(plumbing.Object) error {
		count++
		return nil
	})

	c.Assert(count, Equals, 31)
	c.Assert(mock.PackfileWriterCalled, Equals, true)
}

func (s *RemoteSuite) TestFetchNoErrAlreadyUpToDate(c *C) {
	sto := memory.NewStorage()
	r := newRemote(sto, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	c.Assert(r.Connect(), IsNil)

	o := &FetchOptions{
		RefSpecs: []config.RefSpec{FixRefSpec},
	}

	err := r.Fetch(o)
	c.Assert(err, IsNil)
	err = r.Fetch(o)
	c.Assert(err, Equals, NoErrAlreadyUpToDate)
}

func (s *RemoteSuite) TestHead(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	err := r.Connect()
	c.Assert(err, IsNil)
	c.Assert(r.Head().Hash(), Equals, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
}

func (s *RemoteSuite) TestRef(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	err := r.Connect()
	c.Assert(err, IsNil)

	ref, err := r.Ref(plumbing.HEAD, false)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, plumbing.HEAD)

	ref, err = r.Ref(plumbing.HEAD, true)
	c.Assert(err, IsNil)
	c.Assert(ref.Name(), Equals, plumbing.ReferenceName("refs/heads/master"))
}

func (s *RemoteSuite) TestRefs(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	r.upSrv = &MockGitUploadPackService{}

	err := r.Connect()
	c.Assert(err, IsNil)

	iter, err := r.Refs()
	c.Assert(err, IsNil)
	c.Assert(iter, NotNil)
}

func (s *RemoteSuite) TestString(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: RepositoryFixture})
	c.Assert(r.String(), Equals, ""+
		"foo\thttps://github.com/git-fixtures/basic.git (fetch)\n"+
		"foo\thttps://github.com/git-fixtures/basic.git (push)",
	)
}
