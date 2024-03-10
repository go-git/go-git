package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type RemoteSuite struct {
	BaseSuite
}

var _ = Suite(&RemoteSuite{})

func (s *RemoteSuite) TestFetchInvalidEndpoint(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"http://\\"}})
	err := r.Fetch(&FetchOptions{RemoteName: "foo"})
	c.Assert(err, ErrorMatches, ".*invalid character.*")
}

func (s *RemoteSuite) TestFetchNonExistentEndpoint(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"ssh://non-existent/foo.git"}})
	err := r.Fetch(&FetchOptions{})
	c.Assert(err, NotNil)
}

func (s *RemoteSuite) TestFetchInvalidSchemaEndpoint(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"qux://foo"}})
	err := r.Fetch(&FetchOptions{})
	c.Assert(err, ErrorMatches, ".*unsupported scheme.*")
}

func (s *RemoteSuite) TestFetchOverriddenEndpoint(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"http://perfectly-valid-url.example.com"}})
	err := r.Fetch(&FetchOptions{RemoteURL: "http://\\"})
	c.Assert(err, ErrorMatches, ".*invalid character.*")
}

func (s *RemoteSuite) TestFetchInvalidFetchOptions(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"qux://foo"}})
	invalid := config.RefSpec("^*$ñ")
	err := r.Fetch(&FetchOptions{RefSpecs: []config.RefSpec{invalid}})
	c.Assert(err, Equals, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestFetchWildcard(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	s.testFetch(c, r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
}

func (s *RemoteSuite) TestFetchExactSHA1(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	})

	s.testFetch(c, r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("35e85108805c84807bc66a02d91535e1e24b38b9:refs/heads/foo"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/foo", "35e85108805c84807bc66a02d91535e1e24b38b9"),
	})
}

func (s *RemoteSuite) TestFetchExactSHA1_NotSoported(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("35e85108805c84807bc66a02d91535e1e24b38b9:refs/heads/foo"),
		},
	})

	c.Assert(err, Equals, ErrExactSHA1NotSupported)

}

func (s *RemoteSuite) TestFetchWildcardTags(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(c, r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
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

func (s *RemoteSuite) TestFetch(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(c, r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/master:refs/remotes/origin/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
	})
}

func (s *RemoteSuite) TestFetchToNewBranch(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(c, r, &FetchOptions{
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

func (s *RemoteSuite) TestFetchToNewBranchWithAllTags(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(c, r, &FetchOptions{
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

func (s *RemoteSuite) TestFetchNonExistentReference(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/foo:refs/remotes/origin/foo"),
		},
	})

	c.Assert(err, ErrorMatches, "couldn't find remote ref.*")
	c.Assert(errors.Is(err, NoMatchingRefSpecError{}), Equals, true)
}

func (s *RemoteSuite) TestFetchContext(c *C) {
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
	c.Assert(err, IsNil)
}

func (s *RemoteSuite) TestFetchContextCanceled(c *C) {
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
	c.Assert(err, Equals, context.Canceled)
}

func (s *RemoteSuite) TestFetchWithAllTags(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(c, r, &FetchOptions{
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

func (s *RemoteSuite) TestFetchWithNoTags(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetLocalRepositoryURL(fixtures.ByTag("tags").One())},
	})

	s.testFetch(c, r, &FetchOptions{
		Tags: NoTags,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
	})

}

func (s *RemoteSuite) TestFetchWithDepth(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	s.testFetch(c, r, &FetchOptions{
		Depth: 1,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/remotes/origin/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})

	c.Assert(r.s.(*memory.Storage).Objects, HasLen, 18)
}

func (s *RemoteSuite) TestFetchWithDepthChange(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	s.testFetch(c, r, &FetchOptions{
		Depth: 1,
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/master:refs/heads/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
	c.Assert(r.s.(*memory.Storage).Commits, HasLen, 1)

	s.testFetch(c, r, &FetchOptions{
		Depth: 3,
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/master:refs/heads/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
	c.Assert(r.s.(*memory.Storage).Commits, HasLen, 3)
}

func (s *RemoteSuite) testFetch(c *C, r *Remote, o *FetchOptions, expected []*plumbing.Reference) {
	err := r.Fetch(o)
	c.Assert(err, IsNil)

	var refs int
	l, err := r.s.IterReferences()
	c.Assert(err, IsNil)
	l.ForEach(func(r *plumbing.Reference) error { refs++; return nil })

	c.Assert(refs, Equals, len(expected))

	for _, exp := range expected {
		r, err := r.s.Reference(exp.Name())
		c.Assert(err, IsNil)
		c.Assert(exp.String(), Equals, r.String())
	}
}

func (s *RemoteSuite) TestFetchWithProgress(c *C) {
	url := s.GetBasicLocalRepositoryURL()
	sto := memory.NewStorage()
	buf := bytes.NewBuffer(nil)

	r := NewRemote(sto, &config.RemoteConfig{Name: "foo", URLs: []string{url}})

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
	fs, clean := s.TemporalFilesystem()
	defer clean()

	fss := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	mock := &mockPackfileWriter{Storer: fss}

	url := s.GetBasicLocalRepositoryURL()
	r := NewRemote(mock, &config.RemoteConfig{Name: "foo", URLs: []string{url}})

	refspec := config.RefSpec("+refs/heads/*:refs/remotes/origin/*")
	err := r.Fetch(&FetchOptions{
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

func (s *RemoteSuite) TestFetchNoErrAlreadyUpToDateButStillUpdateLocalRemoteRefs(c *C) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	o := &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}

	err := r.Fetch(o)
	c.Assert(err, IsNil)

	// Simulate an out of date remote ref even though we have the new commit locally
	r.s.SetReference(plumbing.NewReferenceFromStrings(
		"refs/remotes/origin/master", "918c48b83bd081e863dbe1b80f8998f058cd8294",
	))

	err = r.Fetch(o)
	c.Assert(err, IsNil)

	exp := plumbing.NewReferenceFromStrings(
		"refs/remotes/origin/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
	)

	ref, err := r.s.Reference("refs/remotes/origin/master")
	c.Assert(err, IsNil)
	c.Assert(exp.String(), Equals, ref.String())
}

func (s *RemoteSuite) TestFetchNoErrAlreadyUpToDateWithNonCommitObjects(c *C) {
	fixture := fixtures.ByTag("tags").One()
	url := s.GetLocalRepositoryURL(fixture)
	s.doTestFetchNoErrAlreadyUpToDate(c, url)
}

func (s *RemoteSuite) doTestFetchNoErrAlreadyUpToDate(c *C, url string) {
	r := NewRemote(memory.NewStorage(), &config.RemoteConfig{URLs: []string{url}})

	o := &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}

	err := r.Fetch(o)
	c.Assert(err, IsNil)
	err = r.Fetch(o)
	c.Assert(err, Equals, NoErrAlreadyUpToDate)
}

func (s *RemoteSuite) testFetchFastForward(c *C, sto storage.Storer) {
	r := NewRemote(sto, &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	s.testFetch(c, r, &FetchOptions{
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
	c.Assert(err, Equals, ErrForceNeeded)

	// And that forcing it fixes the problem.
	err = r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/branch:refs/heads/master"),
		},
	})
	c.Assert(err, IsNil)

	// Now test that a fast-forward, non-force fetch works.
	r.s.SetReference(plumbing.NewReferenceFromStrings(
		"refs/heads/master", "918c48b83bd081e863dbe1b80f8998f058cd8294",
	))
	s.testFetch(c, r, &FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/master:refs/heads/master"),
		},
	}, []*plumbing.Reference{
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	})
}

func (s *RemoteSuite) TestFetchFastForwardMem(c *C) {
	s.testFetchFastForward(c, memory.NewStorage())
}

func (s *RemoteSuite) TestFetchFastForwardFS(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	fss := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	// This exercises `storage.filesystem.Storage.CheckAndSetReference()`.
	s.testFetchFastForward(c, fss)
}

func (s *RemoteSuite) TestString(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{
		Name: "foo",
		URLs: []string{"https://github.com/git-fixtures/basic.git"},
	})

	c.Assert(r.String(), Equals, ""+
		"foo\thttps://github.com/git-fixtures/basic.git (fetch)\n"+
		"foo\thttps://github.com/git-fixtures/basic.git (push)",
	)
}

func (s *RemoteSuite) TestPushToEmptyRepository(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	server, err := PlainInit(url, true)
	c.Assert(err, IsNil)

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
	c.Assert(err, IsNil)

	iter, err := r.s.IterReferences()
	c.Assert(err, IsNil)

	expected := make(map[string]string)
	iter.ForEach(func(ref *plumbing.Reference) error {
		if !ref.Name().IsBranch() {
			return nil
		}

		expected[ref.Name().String()] = ref.Hash().String()
		return nil
	})
	c.Assert(err, IsNil)

	AssertReferences(c, server, expected)

}

func (s *RemoteSuite) TestPushContext(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	_, err := PlainInit(url, true)
	c.Assert(err, IsNil)

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
	c.Assert(err, IsNil)

	eventually(c, func() bool {
		return runtime.NumGoroutine() <= numGoroutines
	})
}

func eventually(c *C, condition func() bool) {
	select {
	case <-time.After(5 * time.Second):
	default:
		if condition() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	c.Assert(condition(), Equals, true)
}

func (s *RemoteSuite) TestPushContextCanceled(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	_, err := PlainInit(url, true)
	c.Assert(err, IsNil)

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
	c.Assert(err, Equals, context.Canceled)

	eventually(c, func() bool {
		return runtime.NumGoroutine() <= numGoroutines
	})
}

func (s *RemoteSuite) TestPushTags(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	server, err := PlainInit(url, true)
	c.Assert(err, IsNil)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{"refs/tags/*:refs/tags/*"},
	})
	c.Assert(err, IsNil)

	AssertReferences(c, server, map[string]string{
		"refs/tags/lightweight-tag": "f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
		"refs/tags/annotated-tag":   "b742a2a9fa0afcfa9a6fad080980fbc26b007c69",
		"refs/tags/commit-tag":      "ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc",
		"refs/tags/blob-tag":        "fe6cb94756faa81e5ed9240f9191b833db5f40ae",
		"refs/tags/tree-tag":        "152175bf7e5580299fa1f0ba41ef6474cc043b70",
	})
}

func (s *RemoteSuite) TestPushFollowTags(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	server, err := PlainInit(url, true)
	c.Assert(err, IsNil)

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
	c.Assert(err, IsNil)

	initialTag, err := localRepo.CreateTag(
		"initial-commit",
		plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		&CreateTagOptions{
			Message: "a tag for the initial commit",
		},
	)
	c.Assert(err, IsNil)

	_, err = localRepo.CreateTag(
		"master-tag",
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		&CreateTagOptions{
			Message: "a tag with a commit not reachable from branch",
		},
	)
	c.Assert(err, IsNil)

	err = r.Push(&PushOptions{
		RefSpecs:   []config.RefSpec{"+refs/heads/branch:refs/heads/branch"},
		FollowTags: true,
	})
	c.Assert(err, IsNil)

	AssertReferences(c, server, map[string]string{
		"refs/heads/branch":        "e8d3ffab552895c19b9fcf7aa264d277cde33881",
		"refs/tags/tip":            tipTag.Hash().String(),
		"refs/tags/initial-commit": initialTag.Hash().String(),
	})

	AssertReferencesMissing(c, server, []string{
		"refs/tags/master-tag",
	})
}

func (s *RemoteSuite) TestPushNoErrAlreadyUpToDate(c *C) {
	fs := fixtures.Basic().One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{fs.Root()},
	})

	err := r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/*:refs/heads/*"},
	})
	c.Assert(err, Equals, NoErrAlreadyUpToDate)
}

func (s *RemoteSuite) TestPushDeleteReference(c *C) {
	fs := fixtures.Basic().One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	url, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(url, true, &CloneOptions{
		URL: fs.Root(),
	})
	c.Assert(err, IsNil)

	remote, err := r.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)

	err = remote.Push(&PushOptions{
		RefSpecs: []config.RefSpec{":refs/heads/branch"},
	})
	c.Assert(err, IsNil)

	_, err = sto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)

	_, err = r.Storer.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
}

func (s *RemoteSuite) TestForcePushDeleteReference(c *C) {
	fs := fixtures.Basic().One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	url, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(url, true, &CloneOptions{
		URL: fs.Root(),
	})
	c.Assert(err, IsNil)

	remote, err := r.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)

	err = remote.Push(&PushOptions{
		RefSpecs: []config.RefSpec{":refs/heads/branch"},
		Force:    true,
	})
	c.Assert(err, IsNil)

	_, err = sto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)

	_, err = r.Storer.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
}

func (s *RemoteSuite) TestPushRejectNonFastForward(c *C) {
	fs := fixtures.Basic().One().DotGit()
	server := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	url, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(url, true, &CloneOptions{
		URL: fs.Root(),
	})
	c.Assert(err, IsNil)

	remote, err := r.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)

	branch := plumbing.ReferenceName("refs/heads/branch")
	oldRef, err := server.Reference(branch)
	c.Assert(err, IsNil)
	c.Assert(oldRef, NotNil)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/heads/branch",
	}})
	c.Assert(err, ErrorMatches, "non-fast-forward update: refs/heads/branch")

	newRef, err := server.Reference(branch)
	c.Assert(err, IsNil)
	c.Assert(newRef, DeepEquals, oldRef)
}

func (s *RemoteSuite) TestPushForce(c *C) {
	f := fixtures.Basic().One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	dstFs := f.DotGit()
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())

	url := dstFs.Root()
	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	oldRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, IsNil)
	c.Assert(oldRef, NotNil)

	err = r.Push(&PushOptions{RefSpecs: []config.RefSpec{
		config.RefSpec("+refs/heads/master:refs/heads/branch"),
	}})
	c.Assert(err, IsNil)

	newRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, IsNil)
	c.Assert(newRef, Not(DeepEquals), oldRef)
}

func (s *RemoteSuite) TestPushForceWithOption(c *C) {
	f := fixtures.Basic().One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	dstFs := f.DotGit()
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())

	url := dstFs.Root()
	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	oldRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, IsNil)
	c.Assert(oldRef, NotNil)

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/branch"},
		Force:    true,
	})
	c.Assert(err, IsNil)

	newRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, IsNil)
	c.Assert(newRef, Not(DeepEquals), oldRef)
}

func (s *RemoteSuite) TestPushForceWithLease_success(c *C) {
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
		c.Log("Executing test cases:", tc.desc)

		f := fixtures.Basic().One()
		sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
		dstFs := f.DotGit()
		dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())

		newCommit := plumbing.NewHashReference(
			"refs/heads/branch", plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
		)
		c.Assert(sto.SetReference(newCommit), IsNil)

		ref, err := sto.Reference("refs/heads/branch")
		c.Assert(err, IsNil)
		c.Log(ref.String())

		url := dstFs.Root()
		r := NewRemote(sto, &config.RemoteConfig{
			Name: DefaultRemoteName,
			URLs: []string{url},
		})

		oldRef, err := dstSto.Reference("refs/heads/branch")
		c.Assert(err, IsNil)
		c.Assert(oldRef, NotNil)

		c.Assert(r.Push(&PushOptions{
			RefSpecs:       []config.RefSpec{"refs/heads/branch:refs/heads/branch"},
			ForceWithLease: &ForceWithLease{},
		}), IsNil)

		newRef, err := dstSto.Reference("refs/heads/branch")
		c.Assert(err, IsNil)
		c.Assert(newRef, DeepEquals, newCommit)
	}
}

func (s *RemoteSuite) TestPushForceWithLease_failure(c *C) {
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
		c.Log("Executing test cases:", tc.desc)

		f := fixtures.Basic().One()
		sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())
		c.Assert(sto.SetReference(
			plumbing.NewHashReference(
				"refs/heads/branch", plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
			),
		), IsNil)

		dstFs := f.DotGit()
		dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())
		c.Assert(dstSto.SetReference(
			plumbing.NewHashReference(
				"refs/heads/branch", plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"),
			),
		), IsNil)

		url := dstFs.Root()
		r := NewRemote(sto, &config.RemoteConfig{
			Name: DefaultRemoteName,
			URLs: []string{url},
		})

		oldRef, err := dstSto.Reference("refs/heads/branch")
		c.Assert(err, IsNil)
		c.Assert(oldRef, NotNil)

		err = r.Push(&PushOptions{
			RefSpecs:       []config.RefSpec{"refs/heads/branch:refs/heads/branch"},
			ForceWithLease: &ForceWithLease{},
		})

		c.Assert(err, DeepEquals, errors.New("non-fast-forward update: refs/heads/branch"))

		newRef, err := dstSto.Reference("refs/heads/branch")
		c.Assert(err, IsNil)
		c.Assert(newRef, Not(DeepEquals), plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"))
	}
}

func (s *RemoteSuite) TestPushPrune(c *C) {
	fs := fixtures.Basic().One().DotGit()

	url, clean := s.TemporalDir()
	defer clean()

	server, err := PlainClone(url, true, &CloneOptions{
		URL: fs.Root(),
	})
	c.Assert(err, IsNil)

	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(dir, true, &CloneOptions{
		URL: url,
	})
	c.Assert(err, IsNil)

	tag, err := r.Reference(plumbing.ReferenceName("refs/tags/v1.0.0"), true)
	c.Assert(err, IsNil)

	err = r.DeleteTag("v1.0.0")
	c.Assert(err, IsNil)

	remote, err := r.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	c.Assert(err, IsNil)

	err = remote.Push(&PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/*:refs/heads/*"),
		},
		Prune: true,
	})
	c.Assert(err, Equals, NoErrAlreadyUpToDate)

	AssertReferences(c, server, map[string]string{
		"refs/tags/v1.0.0": tag.Hash().String(),
	})

	err = remote.Push(&PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("*:*"),
		},
		Prune: true,
	})
	c.Assert(err, IsNil)

	AssertReferences(c, server, map[string]string{
		"refs/remotes/origin/master": ref.Hash().String(),
	})

	AssertReferences(c, server, map[string]string{
		"refs/remotes/origin/master": ref.Hash().String(),
	})

	_, err = server.Reference(plumbing.ReferenceName("refs/tags/v1.0.0"), true)
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
}

func (s *RemoteSuite) TestPushNewReference(c *C) {
	fs := fixtures.Basic().One().DotGit()

	url, clean := s.TemporalDir()
	defer clean()

	server, err := PlainClone(url, true, &CloneOptions{
		URL: fs.Root(),
	})
	c.Assert(err, IsNil)

	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(dir, true, &CloneOptions{
		URL: url,
	})
	c.Assert(err, IsNil)

	remote, err := r.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	c.Assert(err, IsNil)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/heads/branch2",
	}})
	c.Assert(err, IsNil)

	AssertReferences(c, server, map[string]string{
		"refs/heads/branch2": ref.Hash().String(),
	})

	AssertReferences(c, r, map[string]string{
		"refs/remotes/origin/branch2": ref.Hash().String(),
	})
}

func (s *RemoteSuite) TestPushNewReferenceAndDeleteInBatch(c *C) {
	fs := fixtures.Basic().One().DotGit()

	url, clean := s.TemporalDir()
	defer clean()

	server, err := PlainClone(url, true, &CloneOptions{
		URL: fs.Root(),
	})
	c.Assert(err, IsNil)

	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(dir, true, &CloneOptions{
		URL: url,
	})
	c.Assert(err, IsNil)

	remote, err := r.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	c.Assert(err, IsNil)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/heads/branch2",
		":refs/heads/branch",
	}})
	c.Assert(err, IsNil)

	AssertReferences(c, server, map[string]string{
		"refs/heads/branch2": ref.Hash().String(),
	})

	AssertReferences(c, r, map[string]string{
		"refs/remotes/origin/branch2": ref.Hash().String(),
	})

	_, err = server.Storer.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, Equals, plumbing.ErrReferenceNotFound)
}

func (s *RemoteSuite) TestPushInvalidEndpoint(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"http://\\"}})
	err := r.Push(&PushOptions{RemoteName: "foo"})
	c.Assert(err, ErrorMatches, ".*invalid character.*")
}

func (s *RemoteSuite) TestPushNonExistentEndpoint(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"ssh://non-existent/foo.git"}})
	err := r.Push(&PushOptions{})
	c.Assert(err, NotNil)
}

func (s *RemoteSuite) TestPushOverriddenEndpoint(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "origin", URLs: []string{"http://perfectly-valid-url.example.com"}})
	err := r.Push(&PushOptions{RemoteURL: "http://\\"})
	c.Assert(err, ErrorMatches, ".*invalid character.*")
}

func (s *RemoteSuite) TestPushInvalidSchemaEndpoint(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "origin", URLs: []string{"qux://foo"}})
	err := r.Push(&PushOptions{})
	c.Assert(err, ErrorMatches, ".*unsupported scheme.*")
}

func (s *RemoteSuite) TestPushInvalidFetchOptions(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"qux://foo"}})
	invalid := config.RefSpec("^*$ñ")
	err := r.Push(&PushOptions{RefSpecs: []config.RefSpec{invalid}})
	c.Assert(err, Equals, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestPushInvalidRefSpec(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"some-url"},
	})

	rs := config.RefSpec("^*$**")
	err := r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{rs},
	})
	c.Assert(err, Equals, config.ErrRefSpecMalformedSeparator)
}

func (s *RemoteSuite) TestPushWrongRemoteName(c *C) {
	r := NewRemote(nil, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"some-url"},
	})

	err := r.Push(&PushOptions{
		RemoteName: "other-remote",
	})
	c.Assert(err, ErrorMatches, ".*remote names don't match.*")
}

func (s *RemoteSuite) TestGetHaves(c *C) {
	f := fixtures.Basic().One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	var localRefs = []*plumbing.Reference{
		plumbing.NewReferenceFromStrings(
			"foo",
			"f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
		),
		plumbing.NewReferenceFromStrings(
			"bar",
			"fe6cb94756faa81e5ed9240f9191b833db5f40ae",
		),
		plumbing.NewReferenceFromStrings(
			"qux",
			"f7b877701fbf855b44c0a9e86f3fdce2c298b07f",
		),
	}

	l, err := getHaves(localRefs, memory.NewStorage(), sto, 0)
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 2)
}

func (s *RemoteSuite) TestList(c *C) {
	repo := fixtures.Basic().One()
	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{repo.URL},
	})

	refs, err := remote.List(&ListOptions{})
	c.Assert(err, IsNil)

	expected := []*plumbing.Reference{
		plumbing.NewSymbolicReference("HEAD", "refs/heads/master"),
		plumbing.NewReferenceFromStrings("refs/heads/master", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
		plumbing.NewReferenceFromStrings("refs/heads/branch", "e8d3ffab552895c19b9fcf7aa264d277cde33881"),
		plumbing.NewReferenceFromStrings("refs/pull/1/head", "b8e471f58bcbca63b07bda20e428190409c2db47"),
		plumbing.NewReferenceFromStrings("refs/pull/2/head", "9632f02833b2f9613afb5e75682132b0b22e4a31"),
		plumbing.NewReferenceFromStrings("refs/pull/2/merge", "c37f58a130ca555e42ff96a071cb9ccb3f437504"),
	}
	c.Assert(len(refs), Equals, len(expected))
	for _, e := range expected {
		found := false
		for _, r := range refs {
			if r.Name() == e.Name() {
				found = true
				c.Assert(r, DeepEquals, e)
			}
		}
		c.Assert(found, Equals, true)
	}
}

func (s *RemoteSuite) TestListPeeling(c *C) {
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
		c.Assert(err, IsNil)
		c.Assert(len(refs) > 0, Equals, true)

		foundPeeled, foundNonPeeled := false, false
		for _, ref := range refs {
			if strings.HasSuffix(ref.Name().String(), peeledSuffix) {
				foundPeeled = true
			} else {
				foundNonPeeled = true
			}
		}

		c.Assert(foundPeeled, Equals, tc.expectPeeled)
		c.Assert(foundNonPeeled, Equals, tc.expectNonPeeled)
	}
}

func (s *RemoteSuite) TestListTimeout(c *C) {
	remote := NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{"https://deelay.me/60000/https://httpstat.us/503"},
	})

	_, err := remote.List(&ListOptions{})

	c.Assert(err, NotNil)
}

func (s *RemoteSuite) TestUpdateShallows(c *C) {
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
	c.Assert(err, IsNil)
	c.Assert(len(shallows), Equals, 0)

	resp := new(packp.UploadPackResponse)
	o := &FetchOptions{
		Depth: 1,
	}

	for _, t := range tests {
		resp.Shallows = t.hashes
		err = remote.updateShallow(o, resp)
		c.Assert(err, IsNil)

		shallow, err := remote.s.Shallow()
		c.Assert(err, IsNil)
		c.Assert(len(shallow), Equals, len(t.result))
		c.Assert(shallow, DeepEquals, t.result)
	}
}

func (s *RemoteSuite) TestUseRefDeltas(c *C) {
	url, clean := s.TemporalDir()
	defer clean()

	_, err := PlainInit(url, true)
	c.Assert(err, IsNil)

	fs := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	ar := packp.NewAdvRefs()

	ar.Capabilities.Add(capability.OFSDelta)
	c.Assert(r.useRefDeltas(ar), Equals, false)

	ar.Capabilities.Delete(capability.OFSDelta)
	c.Assert(r.useRefDeltas(ar), Equals, true)
}

func (s *RemoteSuite) TestPushRequireRemoteRefs(c *C) {
	f := fixtures.Basic().One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	dstFs := f.DotGit()
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())

	url := dstFs.Root()
	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	oldRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, IsNil)
	c.Assert(oldRef, NotNil)

	otherRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/master"))
	c.Assert(err, IsNil)
	c.Assert(otherRef, NotNil)

	err = r.Push(&PushOptions{
		RefSpecs:          []config.RefSpec{"refs/heads/master:refs/heads/branch"},
		RequireRemoteRefs: []config.RefSpec{config.RefSpec(otherRef.Hash().String() + ":refs/heads/branch")},
	})
	c.Assert(err, ErrorMatches, "remote ref refs/heads/branch required to be .* but is .*")

	newRef, err := dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, IsNil)
	c.Assert(newRef, DeepEquals, oldRef)

	err = r.Push(&PushOptions{
		RefSpecs:          []config.RefSpec{"refs/heads/master:refs/heads/branch"},
		RequireRemoteRefs: []config.RefSpec{config.RefSpec(oldRef.Hash().String() + ":refs/heads/branch")},
	})
	c.Assert(err, ErrorMatches, "non-fast-forward update: .*")

	newRef, err = dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, IsNil)
	c.Assert(newRef, DeepEquals, oldRef)

	err = r.Push(&PushOptions{
		RefSpecs:          []config.RefSpec{"refs/heads/master:refs/heads/branch"},
		RequireRemoteRefs: []config.RefSpec{config.RefSpec(oldRef.Hash().String() + ":refs/heads/branch")},
		Force:             true,
	})
	c.Assert(err, IsNil)

	newRef, err = dstSto.Reference(plumbing.ReferenceName("refs/heads/branch"))
	c.Assert(err, IsNil)
	c.Assert(newRef, Not(DeepEquals), oldRef)
}

func (s *RemoteSuite) TestFetchPrune(c *C) {
	fs := fixtures.Basic().One().DotGit()

	url, clean := s.TemporalDir()
	defer clean()

	_, err := PlainClone(url, true, &CloneOptions{
		URL: fs.Root(),
	})
	c.Assert(err, IsNil)

	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(dir, true, &CloneOptions{
		URL: url,
	})
	c.Assert(err, IsNil)

	remote, err := r.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	c.Assert(err, IsNil)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/heads/branch",
	}})
	c.Assert(err, IsNil)

	dirSave, clean := s.TemporalDir()
	defer clean()

	rSave, err := PlainClone(dirSave, true, &CloneOptions{
		URL: url,
	})
	c.Assert(err, IsNil)

	AssertReferences(c, rSave, map[string]string{
		"refs/remotes/origin/branch": ref.Hash().String(),
	})

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		":refs/heads/branch",
	}})

	AssertReferences(c, rSave, map[string]string{
		"refs/remotes/origin/branch": ref.Hash().String(),
	})

	err = rSave.Fetch(&FetchOptions{Prune: true})
	c.Assert(err, IsNil)

	_, err = rSave.Reference("refs/remotes/origin/branch", true)
	c.Assert(err, ErrorMatches, "reference not found")
}

func (s *RemoteSuite) TestFetchPruneTags(c *C) {
	fs := fixtures.Basic().One().DotGit()

	url, clean := s.TemporalDir()
	defer clean()

	_, err := PlainClone(url, true, &CloneOptions{
		URL: fs.Root(),
	})
	c.Assert(err, IsNil)

	dir, clean := s.TemporalDir()
	defer clean()

	r, err := PlainClone(dir, true, &CloneOptions{
		URL: url,
	})
	c.Assert(err, IsNil)

	remote, err := r.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)

	ref, err := r.Reference(plumbing.ReferenceName("refs/heads/master"), true)
	c.Assert(err, IsNil)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/heads/master:refs/tags/v1",
	}})
	c.Assert(err, IsNil)

	dirSave, clean := s.TemporalDir()
	defer clean()

	rSave, err := PlainClone(dirSave, true, &CloneOptions{
		URL: url,
	})
	c.Assert(err, IsNil)

	AssertReferences(c, rSave, map[string]string{
		"refs/tags/v1": ref.Hash().String(),
	})

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		":refs/tags/v1",
	}})

	AssertReferences(c, rSave, map[string]string{
		"refs/tags/v1": ref.Hash().String(),
	})

	err = rSave.Fetch(&FetchOptions{Prune: true, RefSpecs: []config.RefSpec{"refs/tags/*:refs/tags/*"}})
	c.Assert(err, IsNil)

	_, err = rSave.Reference("refs/tags/v1", true)
	c.Assert(err, ErrorMatches, "reference not found")
}

func (s *RemoteSuite) TestCanPushShasToReference(c *C) {
	d, err := os.MkdirTemp("", "TestCanPushShasToReference")
	c.Assert(err, IsNil)
	if err != nil {
		return
	}
	defer os.RemoveAll(d)

	// remote currently forces a plain path for path based remotes inside the PushContext function.
	// This makes it impossible, in the current state to use memfs.
	// For the sake of readability, use the same osFS everywhere and use plain git repositories on temporary files
	remote, err := PlainInit(filepath.Join(d, "remote"), true)
	c.Assert(err, IsNil)
	c.Assert(remote, NotNil)

	repo, err := PlainInit(filepath.Join(d, "repo"), false)
	c.Assert(err, IsNil)
	c.Assert(repo, NotNil)

	sha := CommitNewFile(c, repo, "README.md")

	gitremote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "local",
		URLs: []string{filepath.Join(d, "remote")},
	})
	c.Assert(err, IsNil)
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
	c.Assert(err, IsNil)
	if err != nil {
		return
	}

	ref, err := remote.Reference(plumbing.ReferenceName("refs/heads/branch"), false)
	c.Assert(err, IsNil)
	if err != nil {
		return
	}
	c.Assert(ref.Hash().String(), Equals, sha.String())
}

func (s *RemoteSuite) TestFetchAfterShallowClone(c *C) {
	tempDir, clean := s.TemporalDir()
	defer clean()
	remoteUrl := filepath.Join(tempDir, "remote")
	repoDir := filepath.Join(tempDir, "repo")

	// Create a new repo and add more than 1 commit (so we can have a shallow commit)
	remote, err := PlainInit(remoteUrl, false)
	c.Assert(err, IsNil)
	c.Assert(remote, NotNil)

	_ = CommitNewFile(c, remote, "File1")
	_ = CommitNewFile(c, remote, "File2")

	// Clone the repo with a depth of 1
	repo, err := PlainClone(repoDir, false, &CloneOptions{
		URL:           remoteUrl,
		Depth:         1,
		Tags:          NoTags,
		SingleBranch:  true,
		ReferenceName: "master",
	})
	c.Assert(err, IsNil)

	// Add new commits to the origin (more than 1 so that our next test hits a missing commit)
	_ = CommitNewFile(c, remote, "File3")
	sha4 := CommitNewFile(c, remote, "File4")

	// Try fetch with depth of 1 again (note, we need to ensure no remote branch remains pointing at the old commit)
	r, err := repo.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)
	s.testFetch(c, r, &FetchOptions{
		Depth: 2,
		Tags:  NoTags,

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
	sha5 := CommitNewFile(c, remote, "File5")

	// Try fetch with depth of 2 this time (to reach a commit that we don't have locally)
	r, err = repo.Remote(DefaultRemoteName)
	c.Assert(err, IsNil)
	s.testFetch(c, r, &FetchOptions{
		Depth: 1,
		Tags:  NoTags,

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
