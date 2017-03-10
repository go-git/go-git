package git

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/src-d/go-git-fixtures"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/storage"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-billy.v2/osfs"
)

type RemoteSuite struct {
	BaseSuite
}

var _ = Suite(&RemoteSuite{})

func (s *RemoteSuite) TestFetchInvalidEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux"})
	err := r.Fetch(&FetchOptions{})
	c.Assert(err, ErrorMatches, ".*invalid endpoint.*")
}

func (s *RemoteSuite) TestFetchNonExistentEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "ssh://non-existent/foo.git"})
	err := r.Fetch(&FetchOptions{})
	c.Assert(err, NotNil)
}

func (s *RemoteSuite) TestFetchInvalidSchemaEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux://foo"})
	err := r.Fetch(&FetchOptions{})
	c.Assert(err, ErrorMatches, ".*unsupported scheme.*")
}

func (s *RemoteSuite) TestFetchInvalidFetchOptions(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux://foo"})
	invalid := config.RefSpec("^*$ñ")
	err := r.Fetch(&FetchOptions{RefSpecs: []config.RefSpec{invalid}})
	c.Assert(err, Equals, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestFetch(c *C) {
	url := s.GetBasicLocalRepositoryURL()
	sto := memory.NewStorage()
	r := newRemote(sto, &config.RemoteConfig{Name: "foo", URL: url})

	refspec := config.RefSpec("+refs/heads/*:refs/remotes/origin/*")
	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{refspec},
	})

	c.Assert(err, IsNil)
	c.Assert(sto.Objects, HasLen, 31)

	expectedRefs := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
	}

	for _, exp := range expectedRefs {
		r, _ := sto.Reference(exp.Name())
		c.Assert(exp.String(), Equals, r.String())
	}
}

func (s *RemoteSuite) TestFetchDepth(c *C) {
	url := s.GetBasicLocalRepositoryURL()
	sto := memory.NewStorage()
	r := newRemote(sto, &config.RemoteConfig{Name: "foo", URL: url})

	refspec := config.RefSpec("+refs/heads/*:refs/remotes/origin/*")
	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{refspec},
		Depth:    1,
	})

	c.Assert(err, IsNil)
	c.Assert(sto.Objects, HasLen, 18)

	expectedRefs := []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
	}

	for _, exp := range expectedRefs {
		r, _ := sto.Reference(exp.Name())
		c.Assert(exp.String(), Equals, r.String())
	}

	h, err := sto.Shallow()
	c.Assert(err, IsNil)
	c.Assert(h, HasLen, 2)
	c.Assert(h, DeepEquals, []plumbing.Hash{
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
}

func (s *RemoteSuite) TestFetchWithProgress(c *C) {
	url := s.GetBasicLocalRepositoryURL()
	sto := memory.NewStorage()
	buf := bytes.NewBuffer(nil)

	r := newRemote(sto, &config.RemoteConfig{Name: "foo", URL: url})

	refspec := config.RefSpec("+refs/heads/*:refs/remotes/origin/*")
	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{refspec},
		Progress: buf,
	})

	c.Assert(err, IsNil)
	c.Assert(sto.Objects, HasLen, 31)

	c.Assert(buf.Len(), Not(Equals), 0)
}

type mockPackfileWriter struct {
	storage.Storer
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

	url := s.GetBasicLocalRepositoryURL()
	r := newRemote(mock, &config.RemoteConfig{Name: "foo", URL: url})

	refspec := config.RefSpec("+refs/heads/*:refs/remotes/origin/*")
	err = r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{refspec},
	})

	c.Assert(err, IsNil)

	var count int
	iter, err := mock.IterEncodedObjects(plumbing.AnyObject)
	c.Assert(err, IsNil)

	iter.ForEach(func(plumbing.EncodedObject) error {
		count++
		return nil
	})

	c.Assert(count, Equals, 31)
	c.Assert(mock.PackfileWriterCalled, Equals, true)
}

func (s *RemoteSuite) TestFetchNoErrAlreadyUpToDate(c *C) {
	url := s.GetBasicLocalRepositoryURL()
	s.doTestFetchNoErrAlreadyUpToDate(c, url)
}

func (s *RemoteSuite) TestFetchNoErrAlreadyUpToDateWithNonCommitObjects(c *C) {
	fixture := fixtures.ByTag("tags").One()
	url := s.GetLocalRepositoryURL(fixture)
	s.doTestFetchNoErrAlreadyUpToDate(c, url)
}

func (s *RemoteSuite) doTestFetchNoErrAlreadyUpToDate(c *C, url string) {

	sto := memory.NewStorage()
	r := newRemote(sto, &config.RemoteConfig{Name: "foo", URL: url})

	refspec := config.RefSpec("+refs/heads/*:refs/remotes/origin/*")
	o := &FetchOptions{
		RefSpecs: []config.RefSpec{refspec},
	}

	err := r.Fetch(o)
	c.Assert(err, IsNil)
	err = r.Fetch(o)
	c.Assert(err, Equals, NoErrAlreadyUpToDate)
}

func (s *RemoteSuite) TestString(c *C) {
	r := newRemote(nil, &config.RemoteConfig{
		Name: "foo",
		URL:  "https://github.com/git-fixtures/basic.git",
	})

	c.Assert(r.String(), Equals, ""+
		"foo\thttps://github.com/git-fixtures/basic.git (fetch)\n"+
		"foo\thttps://github.com/git-fixtures/basic.git (push)",
	)
}

func (s *RemoteSuite) TestPushToEmptyRepository(c *C) {
	srcFs := fixtures.Basic().One().DotGit()
	sto, err := filesystem.NewStorage(srcFs)
	c.Assert(err, IsNil)

	dstFs := fixtures.ByTag("empty").One().DotGit()
	url := fmt.Sprintf("file://%s", dstFs.Base())

	r := newRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URL:  url,
	})

	rs := config.RefSpec("refs/heads/*:refs/heads/*")
	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{rs},
	})
	c.Assert(err, IsNil)

	dstSto, err := filesystem.NewStorage(dstFs)
	c.Assert(err, IsNil)
	dstRepo, err := Open(dstSto, nil)
	c.Assert(err, IsNil)

	iter, err := sto.IterReferences()
	c.Assert(err, IsNil)
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if !ref.IsBranch() {
			return nil
		}

		dstRef, err := dstRepo.Reference(ref.Name(), true)
		c.Assert(err, IsNil, Commentf("ref: %s", ref.String()))
		c.Assert(dstRef, DeepEquals, ref)

		return nil
	})
	c.Assert(err, IsNil)
}

func (s *RemoteSuite) TestPushNoErrAlreadyUpToDate(c *C) {
	f := fixtures.Basic().One()
	sto, err := filesystem.NewStorage(f.DotGit())
	c.Assert(err, IsNil)
	url := fmt.Sprintf("file://%s", f.DotGit().Base())
	r := newRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URL:  url,
	})

	rs := config.RefSpec("refs/heads/*:refs/heads/*")
	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{rs},
	})
	c.Assert(err, Equals, NoErrAlreadyUpToDate)
}

func (s *RemoteSuite) TestPushInvalidEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux"})
	err := r.Push(&PushOptions{})
	c.Assert(err, ErrorMatches, ".*invalid endpoint.*")
}

func (s *RemoteSuite) TestPushNonExistentEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "ssh://non-existent/foo.git"})
	err := r.Push(&PushOptions{})
	c.Assert(err, NotNil)
}

func (s *RemoteSuite) TestPushInvalidSchemaEndpoint(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux://foo"})
	err := r.Push(&PushOptions{})
	c.Assert(err, ErrorMatches, ".*unsupported scheme.*")
}

func (s *RemoteSuite) TestPushInvalidFetchOptions(c *C) {
	r := newRemote(nil, &config.RemoteConfig{Name: "foo", URL: "qux://foo"})
	invalid := config.RefSpec("^*$ñ")
	err := r.Push(&PushOptions{RefSpecs: []config.RefSpec{invalid}})
	c.Assert(err, Equals, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestPushInvalidRefSpec(c *C) {
	r := newRemote(nil, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URL:  "file:///some-url",
	})

	rs := config.RefSpec("^*$**")
	err := r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{rs},
	})
	c.Assert(err, Equals, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestPushWrongRemoteName(c *C) {
	r := newRemote(nil, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URL:  "file:///some-url",
	})

	err := r.Push(&PushOptions{
		RemoteName: "other-remote",
	})
	c.Assert(err, ErrorMatches, ".*remote names don't match.*")
}
