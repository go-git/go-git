package transactional

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestReflogSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ReflogSuite))
}

type ReflogSuite struct {
	suite.Suite
}

type noReflogStorage struct {
	sto *memory.Storage
}

func (s noReflogStorage) NewEncodedObject() plumbing.EncodedObject {
	return s.sto.NewEncodedObject()
}

func (s noReflogStorage) RawObjectWriter(typ plumbing.ObjectType, sz int64) (io.WriteCloser, error) {
	return s.sto.RawObjectWriter(typ, sz)
}

func (s noReflogStorage) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	return s.sto.SetEncodedObject(obj)
}

func (s noReflogStorage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	return s.sto.EncodedObject(t, h)
}

func (s noReflogStorage) EncodedObjectSize(h plumbing.Hash) (int64, error) {
	return s.sto.EncodedObjectSize(h)
}

func (s noReflogStorage) AddAlternate(remote string) error {
	return s.sto.AddAlternate(remote)
}

func (s noReflogStorage) HasEncodedObject(h plumbing.Hash) error {
	return s.sto.HasEncodedObject(h)
}

func (s noReflogStorage) IterEncodedObjects(t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	return s.sto.IterEncodedObjects(t)
}

func (s noReflogStorage) SetReference(ref *plumbing.Reference) error {
	return s.sto.SetReference(ref)
}

func (s noReflogStorage) CheckAndSetReference(ref, old *plumbing.Reference) error {
	return s.sto.CheckAndSetReference(ref, old)
}

func (s noReflogStorage) Reference(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	return s.sto.Reference(name)
}

func (s noReflogStorage) IterReferences() (storer.ReferenceIter, error) {
	return s.sto.IterReferences()
}

func (s noReflogStorage) CountLooseRefs() (int, error) {
	return s.sto.CountLooseRefs()
}

func (s noReflogStorage) PackRefs() error {
	return s.sto.PackRefs()
}

func (s noReflogStorage) RemoveReference(name plumbing.ReferenceName) error {
	return s.sto.RemoveReference(name)
}

func (s noReflogStorage) SetIndex(idx *index.Index) error {
	return s.sto.SetIndex(idx)
}

func (s noReflogStorage) Index() (*index.Index, error) {
	return s.sto.Index()
}

func (s noReflogStorage) SetShallow(commits []plumbing.Hash) error {
	return s.sto.SetShallow(commits)
}

func (s noReflogStorage) Shallow() ([]plumbing.Hash, error) {
	return s.sto.Shallow()
}

func (s noReflogStorage) SetConfig(cfg *config.Config) error {
	return s.sto.SetConfig(cfg)
}

func (s noReflogStorage) Config() (*config.Config, error) {
	return s.sto.Config()
}

func (s noReflogStorage) Module(name string) (storage.Storer, error) {
	return s.sto.Module(name)
}

func newEntry(msg string) *reflog.Entry {
	return &reflog.Entry{
		OldHash: plumbing.ZeroHash,
		NewHash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Committer: reflog.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Unix(1234567890, 0).UTC(),
		},
		Message: msg,
	}
}

var testRef = plumbing.ReferenceName("refs/heads/main")

func (s *ReflogSuite) TestReflogReadsMergeBaseAndTemporal() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	baseEntry := newEntry("commit: base")
	s.NoError(base.AppendReflog(testRef, baseEntry))

	tempEntry := newEntry("commit: temporal")
	s.NoError(rs.AppendReflog(testRef, tempEntry))

	entries, err := rs.Reflog(testRef)
	s.NoError(err)
	s.Len(entries, 2)
	s.Equal("commit: base", entries[0].Message)
	s.Equal("commit: temporal", entries[1].Message)
}

func (s *ReflogSuite) TestReflogBaseOnly() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(testRef, newEntry("commit: base")))

	entries, err := rs.Reflog(testRef)
	s.NoError(err)
	s.Len(entries, 1)
	s.Equal("commit: base", entries[0].Message)
}

func (s *ReflogSuite) TestReflogTemporalOnly() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(rs.AppendReflog(testRef, newEntry("commit: temporal")))

	entries, err := rs.Reflog(testRef)
	s.NoError(err)
	s.Len(entries, 1)
	s.Equal("commit: temporal", entries[0].Message)
}

func (s *ReflogSuite) TestReflogEmpty() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	entries, err := rs.Reflog(testRef)
	s.NoError(err)
	s.Empty(entries)
}

func (s *ReflogSuite) TestDeleteHidesBase() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(testRef, newEntry("commit: base")))
	s.NoError(rs.DeleteReflog(testRef))

	entries, err := rs.Reflog(testRef)
	s.NoError(err)
	s.Empty(entries)
}

func (s *ReflogSuite) TestDeleteThenAppend() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(testRef, newEntry("commit: old")))
	s.NoError(rs.DeleteReflog(testRef))
	s.NoError(rs.AppendReflog(testRef, newEntry("commit: new")))

	entries, err := rs.Reflog(testRef)
	s.NoError(err)
	s.Len(entries, 1)
	s.Equal("commit: new", entries[0].Message)
}

func (s *ReflogSuite) TestCommitFlushesAppends() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(testRef, newEntry("commit: base")))
	s.NoError(rs.AppendReflog(testRef, newEntry("commit: temporal")))
	s.NoError(rs.Commit())

	// Base should now have both entries.
	entries, err := base.Reflog(testRef)
	s.NoError(err)
	s.Len(entries, 2)
	s.Equal("commit: base", entries[0].Message)
	s.Equal("commit: temporal", entries[1].Message)
}

func (s *ReflogSuite) TestCommitFlushesDeletes() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(testRef, newEntry("commit: base")))
	s.NoError(rs.DeleteReflog(testRef))
	s.NoError(rs.Commit())

	// Base should be empty for this ref.
	entries, err := base.Reflog(testRef)
	s.NoError(err)
	s.Empty(entries)
}

func (s *ReflogSuite) TestCommitDeleteThenAppend() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(testRef, newEntry("commit: old")))
	s.NoError(rs.DeleteReflog(testRef))
	s.NoError(rs.AppendReflog(testRef, newEntry("commit: new")))
	s.NoError(rs.Commit())

	// Base should have only the new entry (old was deleted).
	entries, err := base.Reflog(testRef)
	s.NoError(err)
	s.Len(entries, 1)
	s.Equal("commit: new", entries[0].Message)
}

func (s *ReflogSuite) TestBaseUntouchedBeforeCommit() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(rs.AppendReflog(testRef, newEntry("commit: temporal")))

	// Base should be unaffected before Commit.
	entries, err := base.Reflog(testRef)
	s.NoError(err)
	s.Empty(entries)
}

func (s *ReflogSuite) TestTransactionalStorageDoesNotExposeReflogWithoutSupport() {
	base := memory.NewStorage()
	temporal := noReflogStorage{sto: memory.NewStorage()}

	st := NewStorage(base, temporal)
	defer func() {
		if closer, ok := st.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	_, ok := st.(interface {
		Reflog(plumbing.ReferenceName) ([]*reflog.Entry, error)
	})
	s.False(ok)
}
