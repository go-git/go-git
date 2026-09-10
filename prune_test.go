package git

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type PruneSuite struct {
	BaseSuite
}

func TestPruneSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(PruneSuite))
}

func (s *PruneSuite) testPrune(deleteTime time.Time) {
	srcFs, err := fixtures.ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	var sto storage.Storer = filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

	los := sto.(storer.LooseObjectStorer)
	s.NotNil(los)

	count := 0
	err = los.ForEachObjectHash(func(_ plumbing.Hash) error {
		count++
		return nil
	})
	s.NoError(err)

	r, err := Open(sto, srcFs)
	s.NoError(err)
	s.NotNil(r)
	defer func() { _ = r.Close() }()

	// Remove a branch so we can prune some objects.
	err = sto.RemoveReference(plumbing.ReferenceName("refs/heads/v4"))
	s.NoError(err)
	err = sto.RemoveReference(plumbing.ReferenceName("refs/remotes/origin/v4"))
	s.NoError(err)

	err = r.Prune(PruneOptions{
		OnlyObjectsOlderThan: deleteTime,
		Handler:              r.DeleteObject,
	})
	s.NoError(err)

	newCount := 0
	err = los.ForEachObjectHash(func(_ plumbing.Hash) error {
		newCount++
		return nil
	})
	s.NoError(err)

	if deleteTime.IsZero() {
		s.True(newCount < count)
	} else {
		// Assume a delete time older than any of the objects was passed in.
		s.Equal(count, newCount)
	}
}

func (s *PruneSuite) TestPrune() {
	s.testPrune(time.Time{})
}

func (s *PruneSuite) TestPruneWithNoDelete() {
	s.testPrune(time.Unix(0, 1))
}

func newPruneSymrefRepository(t *testing.T) (*Repository, plumbing.Hash, plumbing.Hash) {
	t.Helper()
	st := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
	r, err := Init(st, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	tree := st.NewEncodedObject()
	require.NoError(t, (&object.Tree{}).Encode(tree))
	treeHash, err := st.SetEncodedObject(tree)
	require.NoError(t, err)
	commit := st.NewEncodedObject()
	require.NoError(t, (&object.Commit{TreeHash: treeHash, Message: "reachable"}).Encode(commit))
	commitHash, err := st.SetEncodedObject(commit)
	require.NoError(t, err)
	return r, commitHash, treeHash
}

func (s *PruneSuite) TestPrunePreservesObjectsThroughRootSymref() {
	r, commit, tree := newPruneSymrefRepository(s.T())
	s.Require().NoError(r.Storer.SetReference(plumbing.NewHashReference("ORIG_HEAD", commit)))
	s.Require().NoError(r.Storer.SetReference(plumbing.NewSymbolicReference("refs/heads/main", "ORIG_HEAD")))
	s.Require().NoError(r.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))

	s.Require().NoError(r.Prune(PruneOptions{Handler: r.DeleteObject}))
	s.Require().NoError(r.Storer.HasEncodedObject(commit))
	s.Require().NoError(r.Storer.HasEncodedObject(tree))
}

func (s *PruneSuite) TestPruneAllowsUnbornAndDanglingSymrefs() {
	r, commit, tree := newPruneSymrefRepository(s.T())
	s.Require().NoError(r.Storer.SetReference(plumbing.NewSymbolicReference("refs/heads/dangling", "refs/heads/missing")))
	s.Require().NoError(r.Prune(PruneOptions{Handler: r.DeleteObject}))
	s.Require().ErrorIs(r.Storer.HasEncodedObject(commit), plumbing.ErrObjectNotFound)
	s.Require().ErrorIs(r.Storer.HasEncodedObject(tree), plumbing.ErrObjectNotFound)
	s.Require().NoError(r.Prune(PruneOptions{Handler: r.DeleteObject}))
}

type pruneReferenceReadError struct {
	*filesystem.Storage
	err error
}

func (s pruneReferenceReadError) Reference(plumbing.ReferenceName) (*plumbing.Reference, error) {
	return nil, s.err
}

func (s *PruneSuite) TestPruneStopsBeforeDeletingOnSymrefReadError() {
	for _, want := range []error{io.ErrUnexpectedEOF, plumbing.ErrInvalidReferenceName} {
		s.Run(want.Error(), func() {
			r, commit, tree := newPruneSymrefRepository(s.T())
			r.Storer = pruneReferenceReadError{r.Storer.(*filesystem.Storage), want}
			called := false
			err := r.Prune(PruneOptions{Handler: func(hash plumbing.Hash) error {
				called = true
				return r.DeleteObject(hash)
			}})
			s.Require().ErrorIs(err, want)
			s.Require().False(called)
			s.Require().NoError(r.Storer.HasEncodedObject(commit))
			s.Require().NoError(r.Storer.HasEncodedObject(tree))
		})
	}
}

func (s *PruneSuite) TestPruneStopsBeforeDeletingOnCyclicSymref() {
	r, commit, tree := newPruneSymrefRepository(s.T())
	s.Require().NoError(r.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/loop")))
	s.Require().NoError(r.Storer.SetReference(plumbing.NewSymbolicReference("refs/heads/loop", plumbing.HEAD)))
	called := false
	err := r.Prune(PruneOptions{Handler: func(plumbing.Hash) error {
		called = true
		return errors.New("destructive handler called")
	}})
	s.Require().ErrorIs(err, storer.ErrMaxResolveRecursion)
	s.Require().False(called)
	s.Require().NoError(r.Storer.HasEncodedObject(commit))
	s.Require().NoError(r.Storer.HasEncodedObject(tree))
}

type pruneRootRefOpenError struct {
	billy.Filesystem
	opened bool
}

func (s *pruneRootRefOpenError) Open(name string) (billy.File, error) {
	if s.Join(name) == "ORIG_HEAD" {
		s.opened = true
		return nil, io.ErrUnexpectedEOF
	}
	return s.Filesystem.Open(name)
}

func (s *PruneSuite) TestMaintenanceStopsOnSymbolicTargetFileReadError() {
	for _, operation := range []string{"prune", "repack"} {
		s.Run(operation, func() {
			r, commit, tree := newPruneSymrefRepository(s.T())
			s.Require().NoError(r.Storer.SetReference(plumbing.NewHashReference("ORIG_HEAD", commit)))
			s.Require().NoError(r.Storer.SetReference(plumbing.NewSymbolicReference("refs/heads/main", "ORIG_HEAD")))
			s.Require().NoError(r.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))
			fs := &pruneRootRefOpenError{Filesystem: r.Storer.(*filesystem.Storage).Filesystem()}
			r.Storer = filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
			called := false
			var err error
			if operation == "prune" {
				err = r.Prune(PruneOptions{Handler: func(hash plumbing.Hash) error {
					called = true
					return r.DeleteObject(hash)
				}})
			} else {
				err = r.RepackObjects(&RepackConfig{})
			}
			s.Require().True(fs.opened)
			s.Require().ErrorIs(err, io.ErrUnexpectedEOF)
			s.Require().False(called)
			s.Require().NoError(r.Storer.HasEncodedObject(commit))
			s.Require().NoError(r.Storer.HasEncodedObject(tree))
			packs, err := r.Storer.(storer.PackedObjectStorer).ObjectPacks()
			s.Require().NoError(err)
			s.Require().Empty(packs)
		})
	}
}
