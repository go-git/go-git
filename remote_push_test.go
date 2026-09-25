package git

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v6"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

func (s *RemoteSuite) TestPushToEmptyRepository() {
	url := s.T().TempDir()
	server, err := PlainInit(url, true)
	s.NoError(err)
	defer func() { _ = server.Close() }()

	srcFs, err := fixtures.Basic().One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

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
	server, err := PlainInit(url, true)
	s.NoError(err)
	defer func() { _ = server.Close() }()

	fs, err := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

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
	server, err := PlainInit(url, true)
	s.Require().NoError(err)
	defer func() { _ = server.Close() }()

	fs, err := fixtures.Basic().One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

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
	server, err := PlainInit(url, true)
	s.NoError(err)
	defer func() { _ = server.Close() }()

	fs, err := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = server.Close() }()

	fs, err := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

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
	defer func() { _ = server.Close() }()

	fs, err := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

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

func (s *RemoteSuite) testPushByOID(oid, refName string) {
	url := s.T().TempDir()

	server, err := PlainInit(url, true)
	s.NoError(err)
	defer func() { _ = server.Close() }()

	fs, err := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec(oid + ":" + refName),
		},
		FollowTags: false,
	})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		refName: oid,
	})
}

func (s *RemoteSuite) TestPushBlobByOID() {
	s.testPushByOID("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", "refs/misc/myblob")
}

func (s *RemoteSuite) TestPushTreeByOID() {
	s.testPushByOID("70846e9a10ef7b41064b40f07713d5b8b9a8fc73", "refs/misc/mytree")
}

func (s *RemoteSuite) TestPushFollowTags() {
	url := s.T().TempDir()
	server, err := PlainInit(url, true)
	s.NoError(err)
	defer func() { _ = server.Close() }()

	fs, err := fixtures.ByURL("https://github.com/git-fixtures/basic.git").One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	localRepo := newRepository(sto, fs)
	defer func() { _ = localRepo.Close() }()
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
	fs, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{fs.Root()},
	})

	err = r.Push(&PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/*:refs/heads/*"},
	})
	s.ErrorIs(err, NoErrAlreadyUpToDate)
}

func (s *RemoteSuite) TestPushDeleteReference() {
	fs, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  fs.Root(),
		Bare: true,
	})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

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
	fs, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)

	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  fs.Root(),
		Bare: true,
	})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

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
	fs, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(err)

	server := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = server.Close() }()

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: fs.Root(), Bare: true})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

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

func (s *RemoteSuite) TestPushRejectExistingTagUpdate() {
	server, local, remote, oldHash, newHash := s.newPushExistingTagUpdate()

	err := local.Storer.SetReference(plumbing.NewHashReference("refs/tags/v1", newHash))
	s.Require().NoError(err)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/tags/v1:refs/tags/v1",
	}})
	s.ErrorContains(err, "tag already exists: refs/tags/v1")

	AssertReferences(s.T(), server, map[string]string{
		"refs/tags/v1": oldHash.String(),
	})
}

func (s *RemoteSuite) TestPushRejectExistingTagUpdateByOID() {
	server, _, remote, oldHash, newHash := s.newPushExistingTagUpdate()

	err := remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		config.RefSpec(newHash.String() + ":refs/tags/v1"),
	}})
	s.ErrorContains(err, "tag already exists: refs/tags/v1")

	AssertReferences(s.T(), server, map[string]string{
		"refs/tags/v1": oldHash.String(),
	})
}

func (s *RemoteSuite) TestPushRejectExistingTagUpdateToAnnotatedTag() {
	server, local, remote, oldHash, newHash := s.newPushExistingTagUpdate()

	_, err := local.CreateTag("v1-annotated", newHash, &CreateTagOptions{
		Tagger:  defaultSignature(),
		Message: "annotated tag",
	})
	s.Require().NoError(err)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/tags/v1-annotated:refs/tags/v1",
	}})
	s.ErrorContains(err, "tag already exists: refs/tags/v1")

	AssertReferences(s.T(), server, map[string]string{
		"refs/tags/v1": oldHash.String(),
	})
}

func (s *RemoteSuite) TestPushForceUpdatesExistingTag() {
	server, local, remote, _, newHash := s.newPushExistingTagUpdate()

	err := local.Storer.SetReference(plumbing.NewHashReference("refs/tags/v1", newHash))
	s.Require().NoError(err)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"+refs/tags/v1:refs/tags/v1",
	}})
	s.NoError(err)

	AssertReferences(s.T(), server, map[string]string{
		"refs/tags/v1": newHash.String(),
	})
}

func (s *RemoteSuite) newPushExistingTagUpdate() (*Repository, *Repository, *Remote, plumbing.Hash, plumbing.Hash) {
	s.T().Helper()

	dir := s.T().TempDir()
	remoteURL := filepath.Join(dir, "remote")

	server, err := PlainInit(remoteURL, true)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = server.Close() })

	local, err := PlainInit(filepath.Join(dir, "local"), false)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = local.Close() })

	oldHash := CommitNewFile(s.T(), local, "old")
	newHash := CommitNewFile(s.T(), local, "new")

	_, err = local.CreateTag("v1", oldHash, nil)
	s.Require().NoError(err)

	remote, err := local.CreateRemote(&config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{remoteURL},
	})
	s.Require().NoError(err)

	err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
		"refs/tags/v1:refs/tags/v1",
	}})
	s.Require().NoError(err)

	return server, local, remote, oldHash, newHash
}

func (s *RemoteSuite) TestPushForce() {
	f := fixtures.Basic().One()
	dotgit, dotgitErr := f.DotGit()
	s.Require().NoError(dotgitErr)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	dstFs, dstErr := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(dstErr)
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())
	defer func() { _ = dstSto.Close() }()

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
	dotgit, dotgitErr := f.DotGit()
	s.Require().NoError(dotgitErr)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	dstFs, dstErr := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(dstErr)
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())
	defer func() { _ = dstSto.Close() }()

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
		dotgit, dotgitErr := f.DotGit()
		s.Require().NoError(dotgitErr)
		sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
		defer func() { _ = sto.Close() }()

		dstFs, dstErr := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
		s.Require().NoError(dstErr)
		dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())
		defer func() { _ = dstSto.Close() }()

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
		dotgit, dotgitErr := f.DotGit()
		s.Require().NoError(dotgitErr)
		sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
		defer func() { _ = sto.Close() }()
		s.NoError(sto.SetReference(
			plumbing.NewHashReference(
				"refs/heads/branch", plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"),
			),
		))

		dstFs, dstErr := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
		s.Require().NoError(dstErr)
		dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())
		defer func() { _ = dstSto.Close() }()
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

func (s *RemoteSuite) TestPushWildcardIgnoresMalformedLocalReferences() {
	for _, prune := range []bool{false, true} {
		s.Run(fmt.Sprintf("prune=%t", prune), func() {
			srcFs, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
			s.Require().NoError(err)
			src, err := Open(filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault()), nil)
			s.Require().NoError(err)
			defer func() { _ = src.Close() }()
			head, err := src.Reference(plumbing.NewBranchReferenceName("master"), true)
			s.Require().NoError(err)
			invalid := []string{"main.lock", ".hidden", "bad~name", "a..b"}
			if runtime.GOOS != "windows" {
				invalid = append(invalid, "a\\b", "a\x01b")
			}
			for _, name := range append(append([]string(nil), invalid...), "@", "-foo") {
				s.Require().NoError(util.WriteFile(srcFs, srcFs.Join("refs", "heads", name),
					[]byte(head.Hash().String()+"\n"), 0o644))
			}

			dstDir := s.T().TempDir()
			dst, err := PlainClone(dstDir, &CloneOptions{URL: srcFs.Root(), Bare: true})
			s.Require().NoError(err)
			defer func() { _ = dst.Close() }()
			for _, name := range []string{"@", "-foo"} {
				s.Require().NoError(dst.Storer.RemoveReference(plumbing.NewBranchReferenceName(name)))
			}
			s.Require().NoError(dst.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("gone"), head.Hash())))
			remote := NewRemote(src.Storer, &config.RemoteConfig{Name: "target", URLs: []string{dstDir}})
			s.Require().NoError(remote.Push(&PushOptions{
				RemoteName: "target", RefSpecs: []config.RefSpec{"refs/heads/*:refs/heads/*"}, Prune: prune,
			}))
			for _, name := range []string{"master", "@", "-foo"} {
				ref, err := dst.Reference(plumbing.NewBranchReferenceName(name), false)
				s.Require().NoError(err)
				s.Equal(head.Hash(), ref.Hash())
			}
			_, err = dst.Reference(plumbing.NewBranchReferenceName("gone"), false)
			if prune {
				s.ErrorIs(err, plumbing.ErrReferenceNotFound)
			} else {
				s.NoError(err)
			}
			iter, err := dst.References()
			s.Require().NoError(err)
			s.Require().NoError(iter.ForEach(func(ref *plumbing.Reference) error {
				s.NoError(ref.Name().Validate())
				return nil
			}))
			for _, name := range invalid {
				_, err := srcFs.Stat(srcFs.Join("refs", "heads", name))
				s.NoError(err)
			}
		})
	}
}

func (s *RemoteSuite) TestPushRejectsMalformedMappedDestinations() {
	srcFs, err := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
	s.Require().NoError(err)
	src := filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())
	head, err := src.Reference(plumbing.NewBranchReferenceName("master"))
	s.Require().NoError(err)
	for _, spec := range []config.RefSpec{
		"refs/heads/master:refs/heads/main.lock",
		"refs/heads/master:refs/heads/bad\nname",
		"refs/heads/*:refs/heads/*.lock",
		config.RefSpec(head.Hash().String() + ":refs/heads/main.lock"),
	} {
		s.Run(spec.String(), func() {
			dir := s.T().TempDir()
			dst, err := PlainInit(dir, true)
			s.Require().NoError(err)
			defer func() { _ = dst.Close() }()
			remote := NewRemote(src, &config.RemoteConfig{Name: DefaultRemoteName, URLs: []string{dir}})
			err = remote.Push(&PushOptions{RefSpecs: []config.RefSpec{
				"refs/heads/master:refs/heads/allowed", spec,
			}})
			s.ErrorIs(err, plumbing.ErrInvalidReferenceName)
			iter, err := dst.References()
			s.Require().NoError(err)
			s.Require().NoError(iter.ForEach(func(ref *plumbing.Reference) error {
				s.Equal(plumbing.HEAD, ref.Name())
				return nil
			}))
		})
	}
}

func (s *RemoteSuite) TestPushRejectsExplicitMalformedSourceBeforePrune() {
	srcFs, err := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
	s.Require().NoError(err)
	src := filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())
	head, err := src.Reference(plumbing.NewBranchReferenceName("master"))
	s.Require().NoError(err)
	for _, name := range []string{"main.lock", ".hidden", "bad~name", "a..b"} {
		fullName := plumbing.NewBranchReferenceName(name)
		s.Require().NoError(util.WriteFile(srcFs, fullName.String(), []byte(head.Hash().String()+"\n"), 0o644))
		for _, prune := range []bool{false, true} {
			s.Run(fmt.Sprintf("%s/prune=%t", name, prune), func() {
				dir := s.T().TempDir()
				dst, err := PlainClone(dir, &CloneOptions{URL: s.GetBasicLocalRepositoryURL(), Bare: true})
				s.Require().NoError(err)
				defer func() { _ = dst.Close() }()
				keep := plumbing.NewHashReference("refs/heads/keep", head.Hash())
				s.Require().NoError(dst.Storer.SetReference(keep))
				remote := NewRemote(src, &config.RemoteConfig{Name: DefaultRemoteName, URLs: []string{dir}})
				err = remote.Push(&PushOptions{Prune: prune, RefSpecs: []config.RefSpec{
					"refs/heads/master:refs/heads/allowed",
					config.RefSpec(fullName.String() + ":refs/heads/keep"),
				}})
				s.ErrorIs(err, plumbing.ErrInvalidReferenceName)
				ref, err := dst.Storer.Reference(keep.Name())
				s.Require().NoError(err)
				s.Equal(keep, ref)
				_, err = dst.Storer.Reference("refs/heads/allowed")
				s.ErrorIs(err, plumbing.ErrReferenceNotFound)
			})
		}
	}
}

func (s *RemoteSuite) TestPushPrune() {
	server, err := PlainClone(s.T().TempDir(), &CloneOptions{URL: s.GetBasicLocalRepositoryURL()})
	s.Require().NoError(err)
	defer func() { _ = server.Close() }()

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  server.wt.Root(),
		Bare: true,
	})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

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
	defer func() { _ = server.Close() }()

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  server.wt.Root(),
		Bare: true,
	})
	s.Require().NoError(err)
	defer func() { _ = r.Close() }()

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
	defer func() { _ = server.Close() }()

	r, err := PlainClone(s.T().TempDir(), &CloneOptions{
		URL:  server.wt.Root(),
		Bare: true,
	})
	s.NoError(err)
	defer func() { _ = r.Close() }()

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

func (s *RemoteSuite) TestUseRefDeltas() {
	url := s.T().TempDir()
	server, err := PlainInit(url, true)
	s.NoError(err)
	defer func() { _ = server.Close() }()

	fs, err := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	s.Require().NoError(err)
	sto := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	r := NewRemote(sto, &config.RemoteConfig{
		Name: DefaultRemoteName,
		URLs: []string{url},
	})

	ar := &packp.AdvRefs{}

	ar.Capabilities.Add(capability.OFSDelta)
	s.False(r.useRefDeltas(ar))

	ar.Capabilities.Delete(capability.OFSDelta)
	s.True(r.useRefDeltas(ar))
}

func (s *RemoteSuite) TestPushRequireRemoteRefs() {
	f := fixtures.Basic().One()
	dotgit, dotgitErr := f.DotGit()
	s.Require().NoError(dotgitErr)
	sto := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())
	defer func() { _ = sto.Close() }()

	dstFs, dstErr := f.DotGit(fixtures.WithTargetDir(s.T().TempDir))
	s.Require().NoError(dstErr)
	dstSto := filesystem.NewStorage(dstFs, cache.NewObjectLRUDefault())
	defer func() { _ = dstSto.Close() }()

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

func (s *RemoteSuite) TestCanPushShasToReference() {
	d := s.T().TempDir()

	// remote currently forces a plain path for path based remotes inside the PushContext function.
	// This makes it impossible, in the current state to use memfs.
	// For the sake of readability, use the same osFS everywhere and use plain git repositories on temporary files
	remote, err := PlainInit(filepath.Join(d, "remote"), true)
	s.NoError(err)
	s.NotNil(remote)
	defer func() { _ = remote.Close() }()

	repo, err := PlainInit(filepath.Join(d, "repo"), false)
	s.NoError(err)
	s.NotNil(repo)
	defer func() { _ = repo.Close() }()

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

func (s *RemoteSuite) TestPushRejectsMalformedWildcardSourcesBeforePrune() {
	for _, tc := range []struct {
		spec   config.RefSpec
		source string
	}{
		{"refs/heads/*.lock:refs/heads/*", "refs/heads/keep.lock"},
		{"refs/heads/.*:refs/heads/*", "refs/heads/.keep"},
		{"refs/heads/a..*:refs/heads/*", "refs/heads/a..keep"},
		{"*.lock:refs/heads/*", "keep.lock"},
	} {
		s.Run(tc.spec.String(), func() {
			srcFS, err := fixtures.Basic().One().DotGit(fixtures.WithTargetDir(s.T().TempDir))
			s.Require().NoError(err)
			src := filesystem.NewStorage(srcFS, cache.NewObjectLRUDefault())
			defer func() { s.Require().NoError(src.Close()) }()
			head, err := src.Reference(plumbing.Master)
			s.Require().NoError(err)
			dir := s.T().TempDir()
			dst, err := PlainClone(dir, &CloneOptions{URL: srcFS.Root(), Bare: true})
			s.Require().NoError(err)
			defer func() { s.Require().NoError(dst.Close()) }()
			keep := plumbing.NewHashReference("refs/heads/keep", head.Hash())
			s.Require().NoError(dst.Storer.SetReference(keep))
			s.Require().NoError(util.WriteFile(srcFS, tc.source, []byte(head.Hash().String()+"\n"), 0o644))
			remote := NewRemote(src, &config.RemoteConfig{Name: DefaultRemoteName, URLs: []string{dir}})
			err = remote.Push(&PushOptions{Prune: true, RefSpecs: []config.RefSpec{
				"refs/heads/master:refs/heads/allowed", tc.spec,
			}})
			s.Require().ErrorIs(err, plumbing.ErrInvalidReferenceName)
			ref, err := dst.Storer.Reference(keep.Name())
			s.Require().NoError(err)
			s.Require().Equal(keep, ref)
			_, err = dst.Storer.Reference("refs/heads/allowed")
			s.Require().ErrorIs(err, plumbing.ErrReferenceNotFound)
		})
	}
}

func (s *RemoteSuite) TestPushPreservesValidWildcardSources() {
	for _, tc := range []struct {
		spec                config.RefSpec
		source, destination plumbing.ReferenceName
	}{
		{"refs/heads/*/topic:refs/heads/*", "refs/heads/feature/topic", "refs/heads/feature"},
		{"*:refs/backup/*", "refs/heads/master", "refs/backup/refs/heads/master"},
		{"refs/heads/-*:refs/heads/kept-*", "refs/heads/-foo", "refs/heads/kept-foo"},
		{"refs/heads/@*:refs/heads/kept-*", "refs/heads/@", "refs/heads/kept-"},
		{"refs/heads/*lock:refs/heads/kept-*", "refs/heads/clock", "refs/heads/kept-c"},
	} {
		s.Run(tc.spec.String(), func() {
			srcFS, err := fixtures.Basic().One().DotGit(fixtures.WithMemFS())
			s.Require().NoError(err)
			src := filesystem.NewStorage(srcFS, cache.NewObjectLRUDefault())
			defer func() { s.Require().NoError(src.Close()) }()
			head, err := src.Reference(plumbing.Master)
			s.Require().NoError(err)
			s.Require().NoError(src.SetReference(plumbing.NewHashReference(tc.source, head.Hash())))
			dir := s.T().TempDir()
			dst, err := PlainInit(dir, true)
			s.Require().NoError(err)
			defer func() { s.Require().NoError(dst.Close()) }()
			remote := NewRemote(src, &config.RemoteConfig{Name: DefaultRemoteName, URLs: []string{dir}})
			s.Require().NoError(remote.Push(&PushOptions{RefSpecs: []config.RefSpec{tc.spec}}))
			ref, err := dst.Storer.Reference(tc.destination)
			s.Require().NoError(err)
			s.Require().Equal(head.Hash(), ref.Hash())
		})
	}
}
