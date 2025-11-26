package git

import (
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type PruneSuite struct {
	BaseSuite
}

func TestPruneSuite(t *testing.T) {
	suite.Run(t, new(PruneSuite))
}

func (s *PruneSuite) testPrune(deleteTime time.Time) {
	srcFs := fixtures.ByTag("unpacked").One().DotGit()
	var sto storage.Storer
	var err error
	sto = filesystem.NewStorage(srcFs, cache.NewObjectLRUDefault())

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
