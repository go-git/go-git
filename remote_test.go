package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type RemoteSuite struct {
	BaseSuite
}

func TestRemoteSuite(t *testing.T) {
	suite.Run(t, new(RemoteSuite))
}

func (s *RemoteSuite) TestFetchInvalidEndpoint() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"http://\\"}})
	err := r.Fetch(&FetchOptions{RemoteName: "foo"})
	s.ErrorContains(err, "invalid character")
}

func (s *RemoteSuite) TestFetchNonExistentEndpoint() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"ssh://non-existent/foo.git"}})
	err := r.Fetch(&FetchOptions{})
	s.NotNil(err)
}

func (s *RemoteSuite) TestFetchInvalidSchemaEndpoint() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"qux://foo"}})
	err := r.Fetch(&FetchOptions{})
	s.ErrorContains(err, "unsupported scheme")
}

func (s *RemoteSuite) TestFetchOverriddenEndpoint() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"http://perfectly-valid-url.example.com"}})
	err := r.Fetch(&FetchOptions{RemoteURL: "http://\\"})
	s.ErrorContains(err, "invalid character")
}

func (s *RemoteSuite) TestFetchInvalidFetchOptions() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"qux://foo"}})
	invalid := config.RefSpec("^*$ñ")
	err := r.Fetch(&FetchOptions{RefSpecs: []config.RefSpec{invalid}})
	s.ErrorIs(err, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestFetchWildcard() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	s.testFetch(r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
}

func (s *RemoteSuite) TestFetchExactSHA1() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	})

	s.testFetch(r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("35e85108805c84807bc66a02d91535e1e24b38b9:refs/heads/foo"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/foo", "35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
}

func (s *RemoteSuite) TestFetchExactSHA1_NotSupported() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("35e85108805c84807bc66a02d91535e1e24b38b9:refs/heads/foo"),
		},
	})

	s.ErrorIs(err, ErrExactSHA1NotSupported)
}

func (s *RemoteSuite) TestFetchWildcardTags() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
		Tags: AllTags,
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
		plumbing.NewReferenceFromStrings("refs/tags/annotated-tag", "b742a2a9fa0afcfa9a6fad080980fbc26b007c69"),
		plumbing.NewReferenceFromStrings("refs/tags/tree-tag", "152175bf7e5580299fa1f0ba41ef6474cc043b70"),
		plumbing.NewReferenceFromStrings("refs/tags/commit-tag", "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"),
		plumbing.NewReferenceFromStrings("refs/tags/blob-tag", "fe6cb94756faa81e5ed9240f9191b833db5f40ae"),
		plumbing.NewReferenceFromStrings("refs/tags/lightweight-tag", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
	})
}

func (s *RemoteSuite) TestFetch() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/master:refs/remotes/origin/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
	})
}

func (s *RemoteSuite) TestFetchToNewBranch() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			// qualified branch to unqualified branch
			"refs/heads/master:foo",
			// unqualified branch to unqualified branch
			"+master:bar",
			// unqualified tag to unqualified branch
			config.RefSpec("tree-tag:tree-tag"),
			// unqualified tag to qualified tag
			config.RefSpec("+commit-tag:refs/tags/renamed-tag"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/foo", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
		plumbing.NewReferenceFromStrings("refs/heads/bar", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
		plumbing.NewReferenceFromStrings("refs/heads/tree-tag", "152175bf7e5580299fa1f0ba41ef6474cc043b70"),
		plumbing.NewReferenceFromStrings("refs/tags/tree-tag", "152175bf7e5580299fa1f0ba41ef6474cc043b70"),
		plumbing.NewReferenceFromStrings("refs/tags/renamed-tag", "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"),
		plumbing.NewReferenceFromStrings("refs/tags/commit-tag", "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"),
	})
}

func (s *RemoteSuite) TestFetchToNewBranchWithAllTags() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(r, &FetchOptions{
		Tags: AllTags,
		RefSpecs: []config.RefSpec{
			// qualified branch to unqualified branch
			"+refs/heads/master:foo",
			// unqualified branch to unqualified branch
			"master:bar",
			// unqualified tag to unqualified branch
			config.RefSpec("+tree-tag:tree-tag"),
			// unqualified tag to qualified tag
			config.RefSpec("commit-tag:refs/tags/renamed-tag"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/foo", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
		plumbing.NewReferenceFromStrings("refs/heads/bar", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
		plumbing.NewReferenceFromStrings("refs/heads/tree-tag", "152175bf7e5580299fa1f0ba41ef6474cc043b70"),
		plumbing.NewReferenceFromStrings("refs/tags/tree-tag", "152175bf7e5580299fa1f0ba41ef6474cc043b70"),
		plumbing.NewReferenceFromStrings("refs/tags/renamed-tag", "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"),
		plumbing.NewReferenceFromStrings("refs/tags/commit-tag", "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"),
		plumbing.NewReferenceFromStrings("refs/tags/annotated-tag", "b742a2a9fa0afcfa9a6fad080980fbc26b007c69"),
		plumbing.NewReferenceFromStrings("refs/tags/blob-tag", "fe6cb94756faa81e5ed9240f9191b833db5f40ae"),
		plumbing.NewReferenceFromStrings("refs/tags/lightweight-tag", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
	})
}

func (s *RemoteSuite) TestFetchNonExistentReference() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/foo:refs/remotes/origin/foo"),
		},
	})

	s.ErrorIs(err, ErrRemoteRefNotFound)
}

func (s *RemoteSuite) TestFetchContext() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := r.FetchContext(ctx, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/master:refs/remotes/origin/master"),
		},
	})
	s.NoError(err)
}

func (s *RemoteSuite) TestFetchContextCanceled() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.FetchContext(ctx, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/master:refs/remotes/origin/master"),
		},
	})
	s.ErrorIs(err, context.Canceled)
}

func (s *RemoteSuite) TestFetchWithAllTags() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(r, &FetchOptions{
		Tags: AllTags,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/master:refs/remotes/origin/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
		plumbing.NewReferenceFromStrings("refs/tags/annotated-tag", "b742a2a9fa0afcfa9a6fad080980fbc26b007c69"),
		plumbing.NewReferenceFromStrings("refs/tags/tree-tag", "152175bf7e5580299fa1f0ba41ef6474cc043b70"),
		plumbing.NewReferenceFromStrings("refs/tags/commit-tag", "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"),
		plumbing.NewReferenceFromStrings("refs/tags/blob-tag", "fe6cb94756faa81e5ed9240f9191b833db5f40ae"),
		plumbing.NewReferenceFromStrings("refs/tags/lightweight-tag", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
	})
}

func (s *RemoteSuite) TestFetchWithNoTags() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(r, &FetchOptions{
		Tags: NoTags,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
	})
}

func (s *RemoteSuite) TestFetchWithDepth() {
	s.T().Skip("We don't support packing shallow-file in go-git server-side" +
		"yet. Since we're using local repositories here, the test will use the" +
		"server-side implementation. See transport/upload_pack.go and" +
		"packfile/encoder.go")
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	s.testFetch(r, &FetchOptions{
		Depth: 1,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})

	s.Len(r.s.(*memory.Storage).Objects, 18)
}

func (s *RemoteSuite) TestFetchWithDepthChange() {
	s.T().Skip("We don't support packing shallow-file in go-git server-side" +
		"yet. Since we're using local repositories here, the test will use the" +
		"server-side implementation. See transport/upload_pack.go and" +
		"packfile/encoder.go")
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	s.testFetch(r, &FetchOptions{
		Depth: 1,
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/master:refs/heads/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
	s.Len(r.s.(*memory.Storage).Commits, 1)

	s.testFetch(r, &FetchOptions{
		Depth: 3,
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/master:refs/heads/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
	s.Len(r.s.(*memory.Storage).Commits, 3)
}

func (s *RemoteSuite) testFetch(r *Remote, o *FetchOptions, expected []*plumbing.Reference) {
	s.T().Helper()
	err := r.Fetch(o)
	s.NoError(err)

	var refs int
	l, err := r.s.IterReferences()
	s.Require().NoError(err)
	err = l.ForEach(func(r *plumbing.Reference) error { refs++; return nil })
	s.Require().NoError(err)

	s.Len(expected, refs)

	for _, exp := range expected {
		r, err := r.s.Reference(exp.Name())
		s.Require().NoError(err)
		s.Equal(exp.String(), r.String())
	}
}

func (s *RemoteSuite) TestFetchOfMissingObjects() {
	dotgit := fixtures.Basic().One().DotGit()
	s.Require().NoError(util.RemoveAll(dotgit, "objects/pack"))

	storage := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	r, err := Open(storage, nil)
	s.Require().NoError(err)

	// Confirm we are missing a commit
	_, err = r.CommitObject(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	s.Require().ErrorIs(err, plumbing.ErrObjectNotFound)

	// Refetch to get all the missing objects
	err = r.Fetch(nil)
	s.NoError(err)

	// Confirm we now have the commit
	_, err = r.CommitObject(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	s.NoError(err)
}

func (s *RemoteSuite) TestFetchWithProgress() {
	// TODO: This test fails because we don't currently support streaming
	// progress messages server-side (i.e. we don't send the progress messages
	// to the client). We support the other direction reading progress messages
	// from the server.
	s.T().Skip("we don't currently support streaming progress messages server-side")
	url := s.GetBasicLocalRepositoryURL()
	sto := memory.NewStorage()
	buf := bytes.NewBuffer(nil)

	r := NewRemote(sto, &config.RemoteConfig{Name: "foo", URLs: []string{url}})

	refspec := config.RefSpec("+refs/heads/*:refs/remotes/origin/*")
	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{refspec},
		Progress: buf,
	})

	s.NoError(err)
	s.Len(sto.Objects, 31)

	s.NotEqual(0, buf.Len())
}

type mockPackfileWriter struct {
	storage.Storer
	PackfileWriterCalled bool
}

func (m *mockPackfileWriter) PackfileWriter() (io.WriteCloser, error) {
	m.PackfileWriterCalled = true
	return m.Storer.(storer.PackfileWriter).PackfileWriter()
}

func (s *RemoteSuite) TestFetchWithPackfileWriter() {
	fs := s.TemporalFilesystem()

	fss := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	mock := &mockPackfileWriter{Storer: fss}

	url := s.GetBasicLocalRepositoryURL()
	r := NewRemote(mock, &config.RemoteConfig{Name: "foo", URLs: []string{url}})

	refspec := config.RefSpec("+refs/heads/*:refs/remotes/origin/*")
	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{refspec},
	})

	s.NoError(err)

	var count int
	iter, err := mock.IterEncodedObjects(plumbing.AnyObject)
	s.NoError(err)

	iter.ForEach(func(plumbing.EncodedObject) error {
		count++
		return nil
	})

	s.Equal(31, count)
	s.True(mock.PackfileWriterCalled)
}

func (s *RemoteSuite) TestFetchNoErrAlreadyUpToDate() {
	url := s.GetBasicLocalRepositoryURL()
	s.doTestFetchNoErrAlreadyUpToDate(url)
}

func (s *RemoteSuite) TestFetchNoErrAlreadyUpToDateButStillUpdateLocalRemoteRefs() {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	o := &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}

	err := r.Fetch(o)
	s.NoError(err)

	// Simulate an out of date remote ref even though we have the new commit locally
	r.s.SetReference(plumbing.NewReferenceFromStrings(
		"refs/remotes/origin/master", "918c48b83bd081e863dbe1b80f8998f058cd8294",
	))

	err = r.Fetch(o)
	s.NoError(err)

	exp := plumbing.NewReferenceFromStrings(
		"refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	)

	ref, err := r.s.Reference("refs/remotes/origin/master")
	s.NoError(err)
	s.Equal(ref.String(), exp.String())
}

func (s *RemoteSuite) TestFetchNoErrAlreadyUpToDateWithNonCommitObjects() {
	fixture := fixtures.ByTag("tags").One()
	url := s.GetLocalRepositoryURL(fixture)
	s.doTestFetchNoErrAlreadyUpToDate(url)
}

func (s *RemoteSuite) doTestFetchNoErrAlreadyUpToDate(url string) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{URLs: []string{url}})

	o := &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}

	err := r.Fetch(o)
	s.NoError(err)
	err = r.Fetch(o)
	s.ErrorIs(err, NoErrAlreadyUpToDate)
}

func (s *RemoteSuite) testFetchFastForward(sto storage.Storer) {
	r := NewRemote(sto, &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	s.testFetch(r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/master:refs/heads/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})

	// First make sure that we error correctly when a force is required.
	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/branch:refs/heads/master"),
		},
	})
	s.ErrorIs(err, ErrForceNeeded)

	// And that forcing it fixes the problem.
	err = r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/branch:refs/heads/master"),
		},
	})
	s.NoError(err)

	// Now test that a fast-forward, non-force fetch works.
	r.s.SetReference(plumbing.NewReferenceFromStrings(
		"refs/heads/master", "918c48b83bd081e863dbe1b80f8998f058cd8294",
	))
	s.testFetch(r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/master:refs/heads/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
}

func (s *RemoteSuite) TestFetchFastForwardMem() {
	s.testFetchFastForward(memory.NewStorage())
}

func (s *RemoteSuite) TestFetchFastForwardFS() {
	fs := s.TemporalFilesystem()

	fss := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	// This exercises `storage.filesystem.Storage.CheckAndSetReference()`.
	s.testFetchFastForward(fss)
}

func (s *RemoteSuite) TestString() {
	r := NewRemote(nil, &config.RemoteConfig{
		Name: "foo",
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	})

	s.Equal(""+
		"foo\thttps://github.com/git-fixtures/basic.git (fetch)\n"+
		"foo\thttps://github.com/git-fixtures/basic.git (push)",
		r.String(),
	)
}

func (s *RemoteSuite) TestPushToEmptyRepository() {
	url := s.T().TempDir()
	server, err := PlainInit(url, true)
	s.NoError(err)

	srcFs := fixtures.Basic().One().DotGit()
	sto := filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	rs := config.RefSpec("refs/heads/*:refs/heads/*")
	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{rs},
	})
	s.NoError(err)

	iter, err := r.s.IterReferences()
	s.NoError(err)

	expected := make(map[string]string)
	iter.ForEach(func(ref *plumbing.Reference) error {
		if !ref.Name().IsBranch() {
			return nil
		}

		expected[ref.Name().String()] = ref.Hash().String()
		return nil
	})
	s.NoError(err)

	AssertReferences(s.T(), server, expected)
}

func (s *RemoteSuite) TestPushContext() {
	url := s.T().TempDir()
	_, err := PlainInit(url, true)
	s.NoError(err)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	numGoroutines := runtime.NumGoroutine()

	err = r.PushContext(ctx, &PushOptions{
		RefSpecs: []config.RefSpec{"refs/tags/*:refs/tags/*"},
	})
	s.NoError(err)

	eventually(s, func() bool {
		return runtime.NumGoroutine() <= numGoroutines
	})
}

func (s *RemoteSuite) TestPushPushOptions() {
	url := s.T().TempDir()
	_, err := PlainInit(url, true)
	s.Require().NoError(err)

	fs := fixtures.Basic().One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	// TODO: Validate the push options was received by the server and implement
	// server-side hooks.
	err = r.Push(&PushOptions{
		Options: []string{
			"iam-a-push-option",
		},
	})
	s.Require().NoError(err)
}

func eventually(s *RemoteSuite, condition func() bool) {
	select {
	case <-time.After(5 * time.Second):
		s.Fail("failed to meet eventual condition")
	default:
		if v := condition(); v {
			s.True(v)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *RemoteSuite) TestPushContextCanceled() {
	url := s.T().TempDir()
	_, err := PlainInit(url, true)
	s.NoError(err)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	numGoroutines := runtime.NumGoroutine()

	err = r.PushContext(ctx, &PushOptions{
		RefSpecs: []config.RefSpec{"refs/tags/*:refs/tags/*"},
	})
	s.ErrorIs(err, context.Canceled)

	eventually(s, func() bool {
		return runtime.NumGoroutine() <= numGoroutines
	})
}

func (s *RemoteSuite) TestPushTags() {
	url := s.T().TempDir()
	server, err := PlainInit(url, true)
	s.NoError(err)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{"refs/tags/*:refs/tags/*"},
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/tags/lightweight-tag": "f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
		"refs/tags/annotated-tag":   "b742a2a9fa0afcfa9a6fad080980fbc26b007c69",
		"refs/tags/commit-tag":      "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc",
		"refs/tags/blob-tag":        "fe6cb94756faa81e5ed9240f9191b833db5f40ae",
		"refs/tags/tree-tag":        "152175bf7e5580299fa1f0ba41ef6474cc043b70",
	})
}

func (s *RemoteSuite) TestPushTagsByOID() {
	url := s.T().TempDir()

	server, err := PlainInit(url, true)
	s.NoError(err)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{
			"f7b877701fbf855b44c0a9e86f3fdce2c298b07f:refs/tags/lightweight-tag-copy",
			"b742a2a9fa0afcfa9a6fad080980fbc26b007c69:refs/tags/annotated-tag-copy",
			"ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc:refs/tags/commit-tag-copy",
			"fe6cb94756faa81e5ed9240f9191b833db5f40ae:refs/tags/blob-tag-copy",
			"152175bf7e5580299fa1f0ba41ef6474cc043b70:refs/tags/tree-tag-copy",
		},
		FollowTags: false,
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/tags/lightweight-tag-copy": "f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
		"refs/tags/annotated-tag-copy":   "b742a2a9fa0afcfa9a6fad080980fbc26b007c69",
		"refs/tags/commit-tag-copy":      "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc",
		"refs/tags/blob-tag-copy":        "fe6cb94756faa81e5ed9240f9191b833db5f40ae",
		"refs/tags/tree-tag-copy":        "152175bf7e5580299fa1f0ba41ef6474cc043b70",
	})
}

func (s *RemoteSuite) TestPushBlobByOID() {
	url := s.T().TempDir()

	server, err := PlainInit(url, true)
	s.NoError(err)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{
			"e69de29bb2d1d6434b8b29ae775ad8c2e48c5391:refs/misc/myblob",
		},
		FollowTags: false,
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/misc/myblob": "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
	})
}

func (s *RemoteSuite) TestPushTreeByOID() {
	url := s.T().TempDir()

	server, err := PlainInit(url, true)
	s.NoError(err)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{
			"70846e9a10ef7b41064b40f07713d5b8b9a8fc73:refs/misc/mytree",
		},
		FollowTags: false,
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/misc/mytree": "70846e9a10ef7b41064b40f07713d5b8b9a8fc73",
	})
}

func (s *RemoteSuite) TestPushFollowTags() {
	url := s.T().TempDir()
	server, err := PlainInit(url, true)
	s.NoError(err)

	fs := fixtures.ByURL("https://github.com/git-fixtures/basic.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	localRepo := newRepository(sto, fs)
	tipTag, err := localRepo.CreateTag(
		"tip",
		plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		&CreateTagOptions{
			Message: "an annotated tag",
		},
	)
	s.NoError(err)

	initialTag, err := localRepo.CreateTag(
		"initial-commit",
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		&CreateTagOptions{
			Message: "a tag for the initial commit",
		},
	)
	s.NoError(err)

	_, err = localRepo.CreateTag(
		"master-tag",
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		&CreateTagOptions{
			Message: "a tag with a commit not reachable from branch",
		},
	)
	s.NoError(err)

	err = r.Push(&PushOptions{
		RefSpecs:   []config.RefSpec{"+refs/heads/branch:refs/heads/branch"},
		FollowTags: true,
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/heads/branch":        "e8d3ffab552895c19b9fcf7aa264d277cde33881",
		"refs/tags/tip":            tipTag.Hash().String(),
		"refs/tags/initial-commit": initialTag.Hash().String(),
	})

	AssertReferencesMissing(s.T(), server, []string{
		"refs/tags/master-tag",
	})
}

func (s *RemoteSuite) TestPushNoErrAlreadyUpToDate() {
	fs := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{fs.Root()},
	})

	err := r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/*:refs/heads/*"},
	})
	s.ErrorIs(err, NoErrAlreadyUpToDate)
}

func (s *RemoteSuite) TestPushDeleteReference() {
	fs := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  fs.Root(),
		Bare: true,
	})
	s.Require().NoError(err)

	remote, err := r.Remote(DefaultRemoteName)
	s.NoError(err)

	err = remote.Push(&PushOptions{
		RefSpecs: []config.RefSpec{":refs/heads/branch"},
	})
	s.NoError(err)

	_, err = sto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)

	_, err = r.Storer.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *RemoteSuite) TestForcePushDeleteReference() {
	fs := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))

	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  fs.Root(),
		Bare: true,
	})
	s.Require().NoError(err)

	remote, err := r.Remote(DefaultRemoteName)
	s.NoError(err)

	err = remote.Push(&PushOptions{
		RefSpecs: []config.RefSpec{":refs/heads/branch"},
		Force:    true,
	})
	s.NoError(err)

	_, err = sto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)

	_, err = r.Storer.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *RemoteSuite) TestPushRejectNonFastForward() {
	fs := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))

	server := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: fs.Root(), Bare: true})
	s.Require().NoError(err)

	remote, err := r.Remote(DefaultRemoteName)
	s.Require().NoError(err)

	branch := plumbing.ReferenceName("refs/heads/branch")
	oldRef, err := server.Reference(branch)
	s.NoError(err)
	s.NotNil(oldRef)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/heads/branch",
	}})
	s.ErrorContains(err, "non-fast-forward update: refs/heads/branch")

	newRef, err := server.Reference(branch)
	s.NoError(err)
	s.Equal(oldRef, newRef)
}

func (s *RemoteSuite) TestPushForce() {
	f := fixtures.Basic().One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	dstFs := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{dstFs.Root()},
	})

	oldRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.NoError(err)
	s.NotNil(oldRef)

	err = r.Push(&PushOptions{RefSpecs: []config.RefSpec{
		config.RefSpec("+refs/heads/master:refs/heads/branch"),
	}})
	s.NoError(err)

	newRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.NoError(err)
	s.NotEqual(oldRef, newRef)
}

func (s *RemoteSuite) TestPushForceWithOption() {
	f := fixtures.Basic().One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	dstFs := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{dstFs.Root()},
	})

	oldRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.NoError(err)
	s.NotNil(oldRef)

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/branch"},
		Force:    true,
	})
	s.NoError(err)

	newRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.NoError(err)
	s.NotEqual(oldRef, newRef)
}

func (s *RemoteSuite) TestPushForceWithLease_success() {
	testCases := []struct {
		desc           string
		forceWithLease ForceWithLease
	}{
		{
			desc:           "no arguments",
			forceWithLease: ForceWithLease{},
		},
		{
			desc: "ref name",
			forceWithLease: ForceWithLease{
				RefName: plumbing.ReferenceName("refs/heads/branch"),
			},
		},
		{
			desc: "ref name and sha",
			forceWithLease: ForceWithLease{
				RefName: plumbing.ReferenceName("refs/heads/branch"),
				Hash:    plumbing.NewHash("e8d3ffab552895c19b9fcf7aa264d277cde33881"),
			},
		},
	}

	for _, tc := range testCases {
		s.T().Log("Executing test cases:", tc.desc)

		f := fixtures.Basic().One()
		sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

		dstFs := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
		dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())

		newCommit := plumbing.NewHashReference(
			"refs/heads/branch", plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		)
		s.Nil(sto.SetReference(newCommit))

		ref, err := sto.Reference("refs/heads/branch")
		s.NoError(err)
		s.T().Log(ref.String())

		r := NewRemote(sto, &config.RemoteConfig{
			Name: DefaultRemoteName,
			URLs: []string{dstFs.Root()},
		})

		oldRef, err := dstSto.Reference("refs/heads/branch")
		s.NoError(err)
		s.NotNil(oldRef)

		s.NoError(r.Push(&PushOptions{
			RefSpecs:       []config.RefSpec{"refs/heads/branch:refs/heads/branch"},
			ForceWithLease: &ForceWithLease{},
		}))

		newRef, err := dstSto.Reference("refs/heads/branch")
		s.NoError(err)
		s.Equal(newCommit, newRef)
	}
}

func (s *RemoteSuite) TestPushForceWithLease_failure() {
	testCases := []struct {
		desc           string
		forceWithLease ForceWithLease
	}{
		{
			desc:           "no arguments",
			forceWithLease: ForceWithLease{},
		},
		{
			desc: "ref name",
			forceWithLease: ForceWithLease{
				RefName: plumbing.ReferenceName("refs/heads/branch"),
			},
		},
		{
			desc: "ref name and sha",
			forceWithLease: ForceWithLease{
				RefName: plumbing.ReferenceName("refs/heads/branch"),
				Hash:    plumbing.NewHash("152175bf7e5580299fa1f0ba41ef6474cc043b70"),
			},
		},
	}

	for _, tc := range testCases {
		s.T().Log("Executing test cases:", tc.desc)

		f := fixtures.Basic().One()
		sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
		s.NoError(sto.SetReference(
			plumbing.NewHashReference(
				"refs/heads/branch", plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
			),
		))

		dstFs := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
		dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())
		s.NoError(dstSto.SetReference(
			plumbing.NewHashReference(
				"refs/heads/branch", plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"),
			),
		))

		r := NewRemote(sto, &config.RemoteConfig{
			Name: DefaultRemoteName,
			URLs: []string{dstFs.Root()},
		})

		oldRef, err := dstSto.Reference("refs/heads/branch")
		s.NoError(err)
		s.NotNil(oldRef)

		err = r.Push(&PushOptions{
			RefSpecs:       []config.RefSpec{"refs/heads/branch:refs/heads/branch"},
			ForceWithLease: &ForceWithLease{},
		})

		s.ErrorContains(err, "non-fast-forward update: refs/heads/branch")

		newRef, err := dstSto.Reference("refs/heads/branch")
		s.NoError(err)
		s.NotEqual(plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"), newRef)
	}
}

func (s *RemoteSuite) TestPushPrune() {
	server, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.Require().NoError(err)

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  server.wt.Root(),
		Bare: true,
	})
	s.Require().NoError(err)

	tag, err := r.Reference(plumbing.ReferenceName("refs/tags/v1.0.0"), true)
	s.NoError(err)

	err = r.DeleteTag("v1.0.0")
	s.NoError(err)

	remote, err := r.Remote(DefaultRemoteName)
	s.NoError(err)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	s.NoError(err)

	err = remote.Push(&PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/*:refs/heads/*"),
		},
		Prune: true,
	})
	s.ErrorIs(err, NoErrAlreadyUpToDate)

	AssertReferences(s.T(), server, map[string]string{
		"refs/tags/v1.0.0": tag.Hash().String(),
	})

	err = remote.Push(&PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("*:*"),
		},
		Prune: true,
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/remotes/origin/master": ref.Hash().String(),
	})

	AssertReferences(s.T(), server, map[string]string{
		"refs/remotes/origin/master": ref.Hash().String(),
	})

	_, err = server.Reference(plumbing.ReferenceName("refs/tags/v1.0.0"), true)
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *RemoteSuite) TestPushNewReference() {
	server, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.Require().NoError(err)

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  server.wt.Root(),
		Bare: true,
	})
	s.Require().NoError(err)

	remote, err := r.Remote(DefaultRemoteName)
	s.NoError(err)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	s.NoError(err)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/heads/branch2",
	}})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/heads/branch2": ref.Hash().String(),
	})

	AssertReferences(s.T(), r, map[string]string{
		"refs/remotes/origin/branch2": ref.Hash().String(),
	})
}

func (s *RemoteSuite) TestPushNewReferenceAndDeleteInBatch() {
	server, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.Require().NoError(err)

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  server.wt.Root(),
		Bare: true,
	})
	s.NoError(err)

	remote, err := r.Remote(DefaultRemoteName)
	s.NoError(err)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	s.NoError(err)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/heads/branch2",
		":refs/heads/branch",
	}})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/heads/branch2": ref.Hash().String(),
	})

	AssertReferences(s.T(), r, map[string]string{
		"refs/remotes/origin/branch2": ref.Hash().String(),
	})

	_, err = server.Storer.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.ErrorIs(err, plumbing.ErrReferenceNotFound)
}

func (s *RemoteSuite) TestPushInvalidEndpoint() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"http://\\"}})
	err := r.Push(&PushOptions{RemoteName: "foo"})
	s.ErrorContains(err, "invalid character")
}

func (s *RemoteSuite) TestPushNonExistentEndpoint() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"ssh://non-existent/foo.git"}})
	err := r.Push(&PushOptions{})
	s.NotNil(err)
}

func (s *RemoteSuite) TestPushOverriddenEndpoint() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "origin", URLs: []string{"http://perfectly-valid-url.example.com"}})
	err := r.Push(&PushOptions{RemoteURL: "http://\\"})
	s.ErrorContains(err, "invalid character")
}

func (s *RemoteSuite) TestPushInvalidSchemaEndpoint() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "origin", URLs: []string{"qux://foo"}})
	err := r.Push(&PushOptions{})
	s.ErrorContains(err, "unsupported scheme")
}

func (s *RemoteSuite) TestPushInvalidFetchOptions() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"qux://foo"}})
	invalid := config.RefSpec("^*$ñ")
	err := r.Push(&PushOptions{RefSpecs: []config.RefSpec{invalid}})
	s.ErrorIs(err, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestPushInvalidRefSpec() {
	r := NewRemote(nil, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"some-url"},
	})

	rs := config.RefSpec("^*$**")
	err := r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{rs},
	})
	s.ErrorIs(err, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestPushWrongRemoteName() {
	r := NewRemote(nil, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"some-url"},
	})

	err := r.Push(&PushOptions{
		RemoteName: "other-remote",
	})
	s.ErrorContains(err, "remote names don't match")
}

func (s *RemoteSuite) TestGetHaves() {
	f := fixtures.Basic().One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	localRefs := []*plumbing.Reference{
		// Exists
		plumbing.NewReferenceFromStrings(
			"foo",
			"b029517f6300c2da0f4b651b8642506cd6aaf45d",
		),
		// Exists
		plumbing.NewReferenceFromStrings(
			"bar",
			"b8e471f58bcbca63b07bda20e428190409c2db47",
		),
		// Doesn't Exist
		plumbing.NewReferenceFromStrings(
			"qux",
			"0000000",
		),
	}

	l, err := getHaves(localRefs, memory.NewStorage(), sto, 0)
	s.NoError(err)
	s.Len(l, 2)
}

func (s *RemoteSuite) TestList() {
	repo := fixtures.Basic().One()
	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{repo.URL},
	})

	refs, err := remote.List(nil)
	s.NoError(err)

	expected := []*plumbing.Reference{
		plumbing.NewSymbolicReference("HEAD", "refs/heads/master"),
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/heads/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewReferenceFromStrings("refs/pull/1/head", "b8e471f58bcbca63b07bda20e428190409c2db47"),
		plumbing.NewReferenceFromStrings("refs/pull/2/head", "9632f02833b2f9613afb5e75682132b0b22e4a31"),
		plumbing.NewReferenceFromStrings("refs/pull/2/merge", "c37f58a130ca555e42ff96a071cb9ccb3f437504"),
	}
	s.Len(expected, len(refs))
	for _, e := range expected {
		found := false
		for _, r := range refs {
			if r.Name() == e.Name() {
				found = true
				s.Equal(e, r)
			}
		}
		s.True(found)
	}
}

func (s *RemoteSuite) TestListPeeling() {
	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"https://github.com/git-fixtures/tags.git"},
	})

	for _, tc := range []struct {
		peelingOption   PeelingOption
		expectPeeled    bool
		expectNonPeeled bool
	}{
		{peelingOption: AppendPeeled, expectPeeled: true, expectNonPeeled: true},
		{peelingOption: IgnorePeeled, expectPeeled: false, expectNonPeeled: true},
		{peelingOption: OnlyPeeled, expectPeeled: true, expectNonPeeled: false},
	} {
		refs, err := remote.List(&ListOptions{
			PeelingOption: tc.peelingOption,
		})
		s.NoError(err)
		s.True(len(refs) > 0)

		foundPeeled, foundNonPeeled := false, false
		for _, ref := range refs {
			if strings.HasSuffix(ref.Name().String(), peeledSuffix) {
				foundPeeled = true
			} else {
				foundNonPeeled = true
			}
		}

		comment := fmt.Sprintf("PeelingOption: %v", tc.peelingOption)
		s.Equal(tc.expectPeeled, foundPeeled, comment)
		s.Equal(tc.expectNonPeeled, foundNonPeeled, comment)
	}
}

func (s *RemoteSuite) TestListTimeout() {
	// Create a server that blocks until the request context is done
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{srv.URL},
	})

	_, err := remote.ListContext(ctx, nil)
	s.ErrorIs(err, context.DeadlineExceeded)
}

/*
func (s *RemoteSuite) TestUpdateShallows() {
	hashes := []plumbing.Hash{
		plumbing.NewHash("0000000000000000000000000000000000000001"),
		plumbing.NewHash("0000000000000000000000000000000000000002"),
		plumbing.NewHash("0000000000000000000000000000000000000003"),
		plumbing.NewHash("0000000000000000000000000000000000000004"),
		plumbing.NewHash("0000000000000000000000000000000000000005"),
		plumbing.NewHash("0000000000000000000000000000000000000006"),
	}

	tests := []struct {
		hashes []plumbing.Hash
		result []plumbing.Hash
	}{
		// add to empty shallows
		{hashes[0:2], hashes[0:2]},
		// add new hashes
		{hashes[2:4], hashes[0:4]},
		// add some hashes already in shallow list
		{hashes[2:6], hashes[0:6]},
		// add all hashes
		{hashes[0:6], hashes[0:6]},
		// add empty list
		{nil, hashes[0:6]},
	}

	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
	})

	shallows, err := remote.s.Shallow()
	s.NoError(err)
	s.Len(shallows, 0)

	resp := new(transport.FetchRequest)
	o := &FetchOptions{
		Depth: 1,
	}

	for _, t := range tests {
		resp.Shallows = t.hashes
		err = remote.updateShallow(o, resp)
		s.NoError(err)

		shallow, err := remote.s.Shallow()
		s.NoError(err)
		s.Len(t.result, len(shallow))
		s.Equal(t.result, shallow)
	}
}
*/

func (s *RemoteSuite) TestUseRefDeltas() {
	url := s.T().TempDir()
	_, err := PlainInit(url, true)
	s.NoError(err)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	ar := packp.NewAdvRefs()

	ar.Capabilities.Add(capability.OFSDelta)
	s.False(r.useRefDeltas(ar))

	ar.Capabilities.Delete(capability.OFSDelta)
	s.True(r.useRefDeltas(ar))
}

func (s *RemoteSuite) TestPushRequireRemoteRefs() {
	f := fixtures.Basic().One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	dstFs := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())

	url := dstFs.Root()
	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	oldRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.NoError(err)
	s.NotNil(oldRef)

	otherRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/master"))
	s.NoError(err)
	s.NotNil(otherRef)

	err = r.Push(&PushOptions{
		RefSpecs:          []config.RefSpec{"refs/heads/master:refs/heads/branch"},
		RequireRemoteRefs: []config.RefSpec{config.RefSpec(otherRef.Hash().String() + ":refs/heads/branch")},
	})
	s.ErrorContains(err, "remote ref refs/heads/branch required to be 6ecf0ef2c2dffb796033e5a02219af86ec6584e5 but is e8d3ffab552895c19b9fcf7aa264d277cde33881")

	newRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.NoError(err)
	s.Equal(oldRef, newRef)

	err = r.Push(&PushOptions{
		RefSpecs:          []config.RefSpec{"refs/heads/master:refs/heads/branch"},
		RequireRemoteRefs: []config.RefSpec{config.RefSpec(oldRef.Hash().String() + ":refs/heads/branch")},
	})
	s.ErrorContains(err, "non-fast-forward update: ")

	newRef, err = dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.NoError(err)
	s.Equal(oldRef, newRef)

	err = r.Push(&PushOptions{
		RefSpecs:          []config.RefSpec{"refs/heads/master:refs/heads/branch"},
		RequireRemoteRefs: []config.RefSpec{config.RefSpec(oldRef.Hash().String() + ":refs/heads/branch")},
		Force:             true,
	})
	s.NoError(err)

	newRef, err = dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	s.NoError(err)
	s.NotEqual(oldRef, newRef)
}

func (s *RemoteSuite) TestFetchPrune() {
	url := s.T().TempDir()
	_, err := PlainClone(url, &CloneOptions{
		URL:  s.GetBasicLocalRepositoryURL(),
		Bare: true,
	})
	s.Require().NoError(err)

	dir := s.T().TempDir()
	r, err := PlainClone(dir, &CloneOptions{
		URL:  url,
		Bare: true,
	})
	s.NoError(err)

	remote, err := r.Remote(DefaultRemoteName)
	s.NoError(err)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	s.NoError(err)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/heads/branch",
	}})
	s.NoError(err)

	dirSave := s.T().TempDir()
	rSave, err := PlainClone(dirSave, &CloneOptions{
		URL:  url,
		Bare: true,
	})
	s.NoError(err)

	AssertReferences(s.T(), rSave, map[string]string{
		"refs/remotes/origin/branch": ref.Hash().String(),
	})

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		":refs/heads/branch",
	}})
	s.NoError(err)

	AssertReferences(s.T(), rSave, map[string]string{
		"refs/remotes/origin/branch": ref.Hash().String(),
	})

	err = rSave.Fetch(&FetchOptions{Prune: true})
	s.NoError(err)

	_, err = rSave.Reference("refs/remotes/origin/branch", true)
	s.ErrorContains(err, "reference not found")
}

func (s *RemoteSuite) TestFetchPruneTags() {
	url := s.T().TempDir()
	_, err := PlainClone(url, &CloneOptions{
		URL:  s.GetBasicLocalRepositoryURL(),
		Bare: true,
	})
	s.Require().NoError(err)

	dir := s.T().TempDir()
	r, err := PlainClone(dir, &CloneOptions{
		URL:  url,
		Bare: true,
	})
	s.NoError(err)

	remote, err := r.Remote(DefaultRemoteName)
	s.NoError(err)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	s.NoError(err)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/tags/v1",
	}})
	s.NoError(err)

	dirSave := s.T().TempDir()
	rSave, err := PlainClone(dirSave, &CloneOptions{
		URL:  url,
		Bare: true,
	})
	s.NoError(err)

	AssertReferences(s.T(), rSave, map[string]string{
		"refs/tags/v1": ref.Hash().String(),
	})

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		":refs/tags/v1",
	}})
	s.NoError(err)

	AssertReferences(s.T(), rSave, map[string]string{
		"refs/tags/v1": ref.Hash().String(),
	})

	err = rSave.Fetch(&FetchOptions{Prune: true, RefSpecs: []config.RefSpec{"refs/tags/*:refs/tags/*"}})
	s.NoError(err)

	_, err = rSave.Reference("refs/tags/v1", true)
	s.ErrorContains(err, "reference not found")
}

func (s *RemoteSuite) TestCanPushShasToReference() {
	d := s.T().TempDir()

	// remote currently forces a plain path for path based remotes inside the PushContext function.
	// This makes it impossible, in the current state to use memfs.
	// For the sake of readability, use the same osFS everywhere and use plain git repositories on temporary files
	remote, err := PlainInit(filepath.Join(d, "remote"), true)
	s.NoError(err)
	s.NotNil(remote)

	repo, err := PlainInit(filepath.Join(d, "repo"), false)
	s.NoError(err)
	s.NotNil(repo)

	sha := CommitNewFile(s.T(), repo, "README.md")

	gitremote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "local",
		URLs: []string{filepath.Join(d, "remote")},
	})
	s.NoError(err)
	if err != nil {
		return
	}

	err = gitremote.Push(&PushOptions{
		RemoteName: "local",
		RefSpecs: []config.RefSpec{
			// TODO: check with short hashes that this is still respected
			config.RefSpec(sha.String() + ":refs/heads/branch"),
		},
	})
	s.NoError(err)
	if err != nil {
		return
	}

	ref, err := remote.Reference(plumbing.ReferenceName("refs/heads/branch"), false)
	s.NoError(err)
	if err != nil {
		return
	}
	s.Equal(sha.String(), ref.Hash().String())
}

func (s *RemoteSuite) TestFetchAfterShallowClone() {
	tempDir := s.T().TempDir()
	remoteUrl := filepath.Join(tempDir, "remote")
	repoDir := filepath.Join(tempDir, "repo")

	// Create a new repo and add more than 1 commit (so we can have a shallow commit)
	remote, err := PlainInit(remoteUrl, false)
	s.Require().NoError(err)
	s.Require().NotNil(remote)

	_ = CommitNewFile(s.T(), remote, "File1")
	_ = CommitNewFile(s.T(), remote, "File2")

	// Clone the repo with a depth of 1
	repo, err := PlainClone(repoDir, &CloneOptions{
		URL:           remoteUrl,
		Depth:         1,
		Tags:          plumbing.NoTags,
		SingleBranch:  true,
		ReferenceName: "master",
	})
	s.NoError(err)

	// Add new commits to the origin (more than 1 so that our next test hits a missing commit)
	_ = CommitNewFile(s.T(), remote, "File3")
	sha4 := CommitNewFile(s.T(), remote, "File4")

	// Try fetch with depth of 1 again (note, we need to ensure no remote branch remains pointing at the old commit)
	r, err := repo.Remote(DefaultRemoteName)
	s.NoError(err)
	s.testFetch(r, &FetchOptions{
		Depth: 2,
		Tags:  plumbing.NoTags,

		RefSpecs: []config.RefSpec{
			"+refs/heads/master:refs/heads/master",
			"+refs/heads/master:refs/remotes/origin/master",
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", sha4.String()),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", sha4.String()),
		plumbing.NewSymbolicReference("HEAD", "refs/heads/master"),
	})

	// Add another commit to the origin
	sha5 := CommitNewFile(s.T(), remote, "File5")

	// Try fetch with depth of 2 this time (to reach a commit that we don't have locally)
	r, err = repo.Remote(DefaultRemoteName)
	s.NoError(err)
	s.testFetch(r, &FetchOptions{
		Depth: 1,
		Tags:  plumbing.NoTags,

		RefSpecs: []config.RefSpec{
			"+refs/heads/master:refs/heads/master",
			"+refs/heads/master:refs/remotes/origin/master",
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", sha5.String()),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", sha5.String()),
		plumbing.NewSymbolicReference("HEAD", "refs/heads/master"),
	})
}

func TestFetchFastForwardForCustomRef(t *testing.T) {
	customRef := "refs/custom/branch"
	// 1. Set up a remote with a URL
	remoteURL := t.TempDir()
	remoteRepo, err := PlainInit(remoteURL, true)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Add a commit with an empty tree to master and custom ref, also set HEAD
	emptyTreeID := writeEmptyTree(t, remoteRepo)
	writeCommitToRef(t, remoteRepo, "refs/heads/master", emptyTreeID, time.Now())
	writeCommitToRef(t, remoteRepo, customRef, emptyTreeID, time.Now())
	if err := remoteRepo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/master")); err != nil {
		t.Fatal(err)
	}

	// 3. Clone repo, then fetch the custom ref
	// Note that using custom ref in ReferenceName has an IsBranch issue
	localRepo, err := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{
		URL: remoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := localRepo.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("%s:%s", customRef, customRef)),
		},
	}); err != nil {
		t.Fatal(err)
	}

	// 4. Make divergent changes
	remoteCommitID := writeCommitToRef(t, remoteRepo, customRef, emptyTreeID, time.Now())
	// Consecutive calls to writeCommitToRef with time.Now() might have the same
	// time value, explicitly set distinct ones to ensure the commit hashes
	// differ
	writeCommitToRef(t, localRepo, customRef, emptyTreeID, time.Now().Add(time.Second))

	// 5. Try to fetch with fast-forward only mode
	remote, err := localRepo.Remote(DefaultRemoteName)
	if err != nil {
		t.Fatal(err)
	}

	err = remote.Fetch(&FetchOptions{RefSpecs: []config.RefSpec{
		config.RefSpec(fmt.Sprintf("%s:%s", customRef, customRef)),
	}})
	if !errors.Is(err, ErrForceNeeded) {
		t.Errorf("expected %v, got %v", ErrForceNeeded, err)
	}

	// 6. Fetch with force
	err = remote.Fetch(&FetchOptions{RefSpecs: []config.RefSpec{
		config.RefSpec(fmt.Sprintf("+%s:%s", customRef, customRef)),
	}})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	// 7. Assert commit ID matches
	ref, err := localRepo.Reference(plumbing.ReferenceName(customRef), true)
	if err != nil {
		t.Fatal(err)
	}
	if remoteCommitID != ref.Hash() {
		t.Errorf("expected %s, got %s", remoteCommitID.String(), ref.Hash().String())
	}
}

func writeEmptyTree(t *testing.T, repo *Repository) plumbing.Hash {
	t.Helper()

	obj := repo.Storer.NewEncodedObject()
	obj.SetType(plumbing.TreeObject)

	tree := object.Tree{Entries: nil}
	if err := tree.Encode(obj); err != nil {
		t.Fatal(err)
	}

	treeID, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}

	return treeID
}

func writeCommitToRef(t *testing.T, repo *Repository, refName string, treeID plumbing.Hash, when time.Time) plumbing.Hash {
	t.Helper()

	ref, err := repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(refName), plumbing.ZeroHash)); err != nil {
				t.Fatal(err)
			}

			ref, err = repo.Reference(plumbing.ReferenceName(refName), true)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			t.Fatal(err)
		}
	}

	commit := &object.Commit{
		TreeHash: treeID,
		Author: object.Signature{
			When: when,
		},
	}
	if !ref.Hash().IsZero() {
		commit.ParentHashes = []plumbing.Hash{ref.Hash()}
	}

	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		t.Fatal(err)
	}

	commitID, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}

	newRef := plumbing.NewHashReference(plumbing.ReferenceName(refName), commitID)
	if err := repo.Storer.CheckAndSetReference(newRef, ref); err != nil {
		t.Fatal(err)
	}

	return commitID
}
