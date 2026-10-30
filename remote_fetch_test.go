package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/trace"
)

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
	// The client-side exact-SHA1 gate (ErrExactSHA1NotSupported) only applies
	// to v0/v1, where the server must advertise allow-*-sha1-in-want. Protocol
	// v2's fetch command accepts any "want <oid>", so there is no unsupported
	// case to assert there; pin this to v0.
	st := memory.NewStorage()
	cfg, err := st.Config()
	s.Require().NoError(err)
	cfg.Protocol.Version = protocol.V0
	s.Require().NoError(st.SetConfig(cfg))

	r := NewRemote(st, &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	err = r.Fetch(&FetchOptions{
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

func (s *RemoteSuite) TestFetchDoesNotClobberExistingTag() {
	sto := memory.NewStorage()

	// A tag the user has already pinned to a specific object.
	pinned := plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "918c48b83bd081e863dbe1b80f8998f058cd8294")
	s.Require().NoError(sto.SetReference(pinned))

	r := NewRemote(sto, &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	// The remote advertises refs/tags/v1.0.0 at a different object. A default
	// fetch auto-follows tags, but must not move a tag that already exists.
	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	})
	s.NoError(err)

	got, err := sto.Reference("refs/tags/v1.0.0")
	s.Require().NoError(err)
	s.Equal(pinned.Hash(), got.Hash())
}

func (s *RemoteSuite) TestFetchTagRefSpecDoesNotClobberExistingTag() {
	sto := memory.NewStorage()

	pinned := plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "918c48b83bd081e863dbe1b80f8998f058cd8294")
	s.Require().NoError(sto.SetReference(pinned))

	r := NewRemote(sto, &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/tags/*:refs/tags/*"),
		},
	})
	s.ErrorIs(err, ErrForceNeeded)

	got, err := sto.Reference("refs/tags/v1.0.0")
	s.Require().NoError(err)
	s.Equal(pinned.Hash(), got.Hash())
}

func (s *RemoteSuite) TestFetchForcedTagRefSpecClobbersExistingTag() {
	sto := memory.NewStorage()

	pinned := plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "918c48b83bd081e863dbe1b80f8998f058cd8294")
	s.Require().NoError(sto.SetReference(pinned))

	r := NewRemote(sto, &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	// A forced tag refspec is the user asking for the update, so it must still
	// move an existing tag.
	err := r.Fetch(&FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/tags/*:refs/tags/*"),
		},
	})
	s.NoError(err)

	got, err := sto.Reference("refs/tags/v1.0.0")
	s.Require().NoError(err)
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), got.Hash())
}

func (s *RemoteSuite) TestFetchAllTagsDoesNotClobberExistingTag() {
	sto := memory.NewStorage()

	pinned := plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "918c48b83bd081e863dbe1b80f8998f058cd8294")
	s.Require().NoError(sto.SetReference(pinned))

	r := NewRemote(sto, &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	err := r.Fetch(&FetchOptions{
		Tags: AllTags,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	})
	s.ErrorIs(err, ErrForceNeeded)

	got, err := sto.Reference("refs/tags/v1.0.0")
	s.Require().NoError(err)
	s.Equal(pinned.Hash(), got.Hash())
}

func (s *RemoteSuite) TestFetchForcedAllTagsClobbersExistingTag() {
	sto := memory.NewStorage()

	pinned := plumbing.NewReferenceFromStrings("refs/tags/v1.0.0", "918c48b83bd081e863dbe1b80f8998f058cd8294")
	s.Require().NoError(sto.SetReference(pinned))

	r := NewRemote(sto, &config.RemoteConfig{
		URLs: []string{s.GetBasicLocalRepositoryURL()},
	})

	err := r.Fetch(&FetchOptions{
		Force: true,
		Tags:  AllTags,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	})
	s.NoError(err)

	got, err := sto.Reference("refs/tags/v1.0.0")
	s.Require().NoError(err)
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), got.Hash())
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
	err = l.ForEach(func(*plumbing.Reference) error { refs++; return nil })
	s.Require().NoError(err)

	s.Len(expected, refs)

	for _, exp := range expected {
		r, err := r.s.Reference(exp.Name())
		s.Require().NoError(err)
		s.Equal(exp.String(), r.String())
	}
}

func (s *RemoteSuite) TestFetchOfMissingObjects() {
	dotgit, err := fixtures.Basic().One().DotGit()
	s.Require().NoError(err)
	s.Require().NoError(util.RemoveAll(dotgit, "objects/pack"))

	storage := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	r, err := Open(storage, nil)
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

	// Confirm we are missing a commit
	_, err = r.CommitObject(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	s.Require().ErrorIs(err, plumbing.ErrObjectNotFound)

	// Refetch to get all the missing objects
	err = r.Fetch(&FetchOptions{})
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
	defer func() { _ = fss.Close() }()
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
	defer func() { _ = fss.Close() }()

	// This exercises `storage.filesystem.Storage.CheckAndSetReference()`.
	s.testFetchFastForward(fss)
}

func (s *RemoteSuite) TestGetHaves() {
	f := fixtures.Basic().One()
	dotgit, dotgitErr := f.DotGit()
	s.Require().NoError(dotgitErr)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

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

func (s *RemoteSuite) TestFetchPrune() {
	url := s.T().TempDir()
	urlRepo, err := PlainClone(url, &CloneOptions{
		URL:  s.GetBasicLocalRepositoryURL(),
		Bare: true,
	})
	s.Require().NoError(err)
	defer func() { _ = urlRepo.Close() }()

	dir := s.T().TempDir()
	r, err := PlainClone(dir, &CloneOptions{
		URL:  url,
		Bare: true,
	})
	s.NoError(err)
	defer func() { _ = r.Close() }()

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
	defer func() { _ = rSave.Close() }()

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
	urlRepo, err := PlainClone(url, &CloneOptions{
		URL:  s.GetBasicLocalRepositoryURL(),
		Bare: true,
	})
	s.Require().NoError(err)
	defer func() { _ = urlRepo.Close() }()

	dir := s.T().TempDir()
	r, err := PlainClone(dir, &CloneOptions{
		URL:  url,
		Bare: true,
	})
	s.NoError(err)
	defer func() { _ = r.Close() }()

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
	defer func() { _ = rSave.Close() }()

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

func (s *RemoteSuite) TestFetchAfterShallowClone() {
	tempDir := s.T().TempDir()
	remoteURL := filepath.Join(tempDir, "remote")
	repoDir := filepath.Join(tempDir, "repo")

	// Create a new repo and add more than 1 commit (so we can have a shallow commit)
	remote, err := PlainInit(remoteURL, false)
	s.Require().NoError(err)
	s.Require().NotNil(remote)
	defer func() { _ = remote.Close() }()

	_ = CommitNewFile(s.T(), remote, "File1")
	_ = CommitNewFile(s.T(), remote, "File2")

	// Clone the repo with a depth of 1
	repo, err := PlainClone(repoDir, &CloneOptions{
		URL:           remoteURL,
		Depth:         1,
		Tags:          plumbing.NoTags,
		SingleBranch:  true,
		ReferenceName: "master",
	})
	s.NoError(err)
	defer func() { _ = repo.Close() }()

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

// TestFetchAfterShallowClone_NoForceRefspec is a regression test for
// https://github.com/go-git/go-git/issues/207.
//
// When a shallow clone is followed by a depth-limited fetch using a plain
// (non-force) refspec, the local ancestor walk would fail with
// plumbing.ErrObjectNotFound because intermediate commits (between the old
// local tip and the new shallow tip) are absent from the local store.
// The fix makes isFastForward aware of shallow boundaries: when ancestry
// cannot be proven due to missing shallow history, it conservatively assumes
// fast-forward (matching the behaviour of git(1)).
func (s *RemoteSuite) TestFetchAfterShallowClone_NoForceRefspec() {
	tempDir := s.T().TempDir()
	remoteURL := filepath.Join(tempDir, "remote")
	repoDir := filepath.Join(tempDir, "repo")

	// Build a remote with two commits so we can take a shallow clone.
	remoteRepo, err := PlainInit(remoteURL, false)
	s.Require().NoError(err)
	defer func() { _ = remoteRepo.Close() }()
	_ = CommitNewFile(s.T(), remoteRepo, "File1")
	_ = CommitNewFile(s.T(), remoteRepo, "File2")

	// Shallow clone at depth=1 — only the latest commit is stored locally.
	repo, err := PlainClone(repoDir, &CloneOptions{
		URL:           remoteURL,
		Depth:         1,
		Tags:          plumbing.NoTags,
		SingleBranch:  true,
		ReferenceName: "master",
	})
	s.Require().NoError(err)
	defer func() { _ = repo.Close() }()

	// Push two more commits to the remote while the local clone is still shallow.
	_ = CommitNewFile(s.T(), remoteRepo, "File3")
	sha4 := CommitNewFile(s.T(), remoteRepo, "File4")

	// Fetch with depth=1 and a plain (non-force) refspec.
	// This means only File4's commit is fetched; File3's commit is absent
	// locally, so the ancestry walk from sha4 back to our current tip would
	// hit a missing object without the fix.
	r, err := repo.Remote(DefaultRemoteName)
	s.Require().NoError(err)

	err = r.Fetch(&FetchOptions{
		Depth: 1,
		Tags:  plumbing.NoTags,
		RefSpecs: []config.RefSpec{
			// No leading '+' — this is a fast-forward-only refspec.
			"refs/heads/master:refs/heads/master",
			"refs/heads/master:refs/remotes/origin/master",
		},
	})
	s.Require().NoError(err, "shallow fetch with non-force refspec must not return an error")

	// Confirm the local branch was updated to the new tip.
	head, err := repo.Reference(plumbing.NewBranchReferenceName("master"), true)
	s.Require().NoError(err)
	s.Equal(sha4, head.Hash(), "local master must point to the new remote tip")
}

func TestFetchFastForwardForCustomRef(t *testing.T) {
	t.Parallel()
	customRef := "refs/custom/branch"
	// 1. Set up a remote with a URL
	remoteURL := t.TempDir()
	remoteRepo, err := PlainInit(remoteURL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remoteRepo.Close() }()

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
	defer func() { _ = localRepo.Close() }()
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

// A refspec names a destination the remote did not choose, so the storer can
// refuse one even after the advertisement has been filtered. That must cost
// the single reference and not the fetch, which is what git does in
// get_fetch_map. Asserted through a real fetch rather than through the
// predicate, because the predicate was never the part that broke.
func (s *RemoteSuite) TestFetchSkipsUnstorableDestination() {
	const traceMode = "GO_GIT_TEST_FETCH_TRACE"
	mode := os.Getenv(traceMode)
	if mode == "" {
		// Trace configuration is process-wide, so each mode runs in isolation
		// from the other parallel repository suites.
		executable, err := os.Executable()
		s.Require().NoError(err)
		for _, mode := range []string{"enabled", "disabled"} {
			s.Run(mode, func() {
				cmd := exec.Command(executable, "-test.v", "-test.run=^TestRemoteSuite$/^TestFetchSkipsUnstorableDestination$")
				cmd.Env = append(os.Environ(), traceMode+"="+mode)
				output, err := cmd.CombinedOutput()
				s.Require().NoError(err, "%s", output)
				s.Contains(string(output), "--- PASS: TestRemoteSuite/TestFetchSkipsUnstorableDestination (")
			})
		}
		return
	}

	var diagnostic bytes.Buffer
	trace.SetLogger(log.New(&diagnostic, "", 0))
	if mode == "enabled" {
		trace.SetTarget(trace.General)
	} else {
		trace.SetTarget(0)
	}

	url := s.GetBasicLocalRepositoryURL()
	// A filesystem storer, because the name gate this exercises is the dotgit
	// one; memory storage accepts any name.
	dir := s.T().TempDir()
	st := filesystem.NewStorage(osfs.New(dir), cache.NewObjectLRUDefault())
	r := NewRemote(st, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	err := r.Fetch(&FetchOptions{RefSpecs: []config.RefSpec{
		// Refused by the format rules, and by the dot-fold rule that only
		// the storer applies. Both must be skipped, not fatal.
		"+refs/heads/master:refs/heads/broken.lock",
		"+refs/heads/master:refs/heads/nz/\u200c./sub",
		"+refs/heads/master:refs/heads/good",
	}})
	s.Require().NoError(err)

	_, err = r.s.Reference("refs/heads/good")
	s.NoError(err, "the usable destination must be stored")

	// Checked on disk rather than through Reference, which gates the same
	// names and would report an error either way.
	for _, p := range []string{"refs/heads/broken.lock", "refs/heads/nz/\u200c./sub"} {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)))
		s.True(os.IsNotExist(err), "%q must not have become a path", p)
	}

	if mode == "enabled" {
		for _, name := range []string{"refs/heads/broken.lock", "refs/heads/nz/\u200c./sub"} {
			err := st.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(name), plumbing.ZeroHash))
			s.Require().ErrorIs(err, plumbing.ErrInvalidReferenceName)
			s.Contains(diagnostic.String(), fmt.Sprintf("ignoring local ref %q from remote ref %q: %q\n",
				name, "refs/heads/master", err.Error()))
		}
	} else {
		s.Empty(diagnostic.String())
	}
}

func (s *RemoteSuite) TestCloneAndFetchSkipUnstorableTags() {
	srcFS, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	src := filesystem.NewStorage(srcFS, cache.NewObjectLRUDefault())
	defer func() { s.Require().NoError(src.Close()) }()
	head, err := src.Reference(plumbing.Master)
	s.Require().NoError(err)
	validTag := plumbing.ReferenceName("refs/tags/valid-name")
	invalidTag := plumbing.ReferenceName("refs/tags/nz/\u200c./tag")
	for _, name := range []plumbing.ReferenceName{validTag, invalidTag} {
		s.Require().NoError(util.WriteFile(srcFS, name.String(), []byte(head.Hash().String()+"\n"), 0o644))
	}

	for _, operation := range []string{"clone", "fetch"} {
		for _, mode := range []struct {
			name string
			tags plumbing.TagMode
		}{{"default", plumbing.InvalidTagMode}, {"all", plumbing.AllTags}, {"none", plumbing.NoTags}} {
			s.Run(operation+"/"+mode.name, func() {
				var dst *Repository
				var err error
				branch := plumbing.Master
				if operation == "clone" {
					dst, err = PlainClone(s.T().TempDir(), &CloneOptions{URL: srcFS.Root(), Bare: true, Tags: mode.tags})
				} else {
					dst, err = PlainInit(s.T().TempDir(), true)
					s.Require().NoError(err)
					remote := NewRemote(dst.Storer, &config.RemoteConfig{Name: DefaultRemoteName, URLs: []string{srcFS.Root()}})
					err = remote.Fetch(&FetchOptions{Tags: mode.tags, RefSpecs: []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"}})
					branch = plumbing.NewRemoteReferenceName(DefaultRemoteName, "master")
				}
				s.Require().NoError(err)
				defer func() { s.Require().NoError(dst.Close()) }()
				ref, err := dst.Storer.Reference(branch)
				s.Require().NoError(err)
				s.Require().Equal(head.Hash(), ref.Hash())
				ref, err = dst.Storer.Reference(validTag)
				if mode.tags == plumbing.NoTags {
					s.Require().ErrorIs(err, plumbing.ErrReferenceNotFound)
				} else {
					s.Require().NoError(err)
					s.Require().Equal(head.Hash(), ref.Hash())
				}
				iter, err := dst.References()
				s.Require().NoError(err)
				s.Require().NoError(iter.ForEach(func(ref *plumbing.Reference) error {
					s.Require().NotEqual(invalidTag, ref.Name())
					return nil
				}))
			})
		}
	}
}

type tagRefErrorStorage struct {
	storage.Storer
	name                        plumbing.ReferenceName
	readErr, writeErr           error
	readFailures, writeFailures int
}

func (s *tagRefErrorStorage) Reference(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	if name == s.name && s.readErr != nil {
		s.readFailures++
		return nil, s.readErr
	}
	return s.Storer.Reference(name)
}

func (s *tagRefErrorStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	if ref.Name() == s.name && s.writeErr != nil {
		s.writeFailures++
		return s.writeErr
	}
	return s.Storer.CheckAndSetReference(ref, old)
}

func (s *RemoteSuite) TestFetchTagStorageErrors() {
	srcFS, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	src := filesystem.NewStorage(srcFS, cache.NewObjectLRUDefault())
	defer func() { s.Require().NoError(src.Close()) }()
	head, err := src.Reference(plumbing.Master)
	s.Require().NoError(err)
	target := plumbing.ReferenceName("refs/tags/rejected")
	validTag := plumbing.ReferenceName("refs/tags/allowed")
	for _, name := range []plumbing.ReferenceName{target, validTag} {
		s.Require().NoError(src.SetReference(plumbing.NewHashReference(name, head.Hash())))
	}

	for _, phase := range []string{"read", "write"} {
		for _, rejectedName := range []bool{false, true} {
			s.Run(fmt.Sprintf("%s/name-error=%t", phase, rejectedName), func() {
				cause := errors.New("reference storage unavailable")
				if rejectedName {
					cause = plumbing.ErrInvalidReferenceName
				}
				dst := memory.NewStorage()
				st := &tagRefErrorStorage{Storer: dst, name: target}
				if phase == "read" {
					st.readErr = fmt.Errorf("reading tag: %w", cause)
				} else {
					st.writeErr = fmt.Errorf("writing tag: %w", cause)
				}
				remote := NewRemote(st, &config.RemoteConfig{Name: DefaultRemoteName, URLs: []string{srcFS.Root()}})
				err := remote.Fetch(&FetchOptions{RefSpecs: []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"}})
				if rejectedName {
					s.Require().NoError(err)
					ref, err := dst.Reference(validTag)
					s.Require().NoError(err)
					s.Require().Equal(head.Hash(), ref.Hash())
				} else {
					s.Require().ErrorIs(err, cause)
				}
				_, err = dst.Reference(target)
				s.Require().ErrorIs(err, plumbing.ErrReferenceNotFound)
				ref, err := dst.Reference(plumbing.NewRemoteReferenceName(DefaultRemoteName, "master"))
				s.Require().NoError(err)
				s.Require().Equal(head.Hash(), ref.Hash())
				if phase == "read" {
					s.Require().Positive(st.readFailures)
					s.Require().Zero(st.writeFailures)
				} else {
					s.Require().Zero(st.readFailures)
					s.Require().Positive(st.writeFailures)
				}
			})
		}
	}
}
