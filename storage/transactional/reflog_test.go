package transactional

import (
	"context"
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

func (s noReflogStorage) RawObjectWriter(ctx context.Context, typ plumbing.ObjectType, sz int64) (io.WriteCloser, error) {
	return s.sto.RawObjectWriter(ctx, typ, sz)
}

func (s noReflogStorage) SetEncodedObject(ctx context.Context, obj plumbing.EncodedObject) (plumbing.Hash, error) {
	return s.sto.SetEncodedObject(ctx, obj)
}

func (s noReflogStorage) EncodedObject(ctx context.Context, t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	return s.sto.EncodedObject(ctx, t, h)
}

func (s noReflogStorage) EncodedObjectSize(ctx context.Context, h plumbing.Hash) (int64, error) {
	return s.sto.EncodedObjectSize(ctx, h)
}

func (s noReflogStorage) AddAlternate(ctx context.Context, remote string) error {
	return s.sto.AddAlternate(ctx, remote)
}

func (s noReflogStorage) HasEncodedObject(ctx context.Context, h plumbing.Hash) error {
	return s.sto.HasEncodedObject(ctx, h)
}

func (s noReflogStorage) IterEncodedObjects(ctx context.Context, t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	return s.sto.IterEncodedObjects(ctx, t)
}

func (s noReflogStorage) SetReference(ctx context.Context, ref *plumbing.Reference) error {
	return s.sto.SetReference(ctx, ref)
}

func (s noReflogStorage) CheckAndSetReference(ctx context.Context, ref, old *plumbing.Reference) error {
	return s.sto.CheckAndSetReference(ctx, ref, old)
}

func (s noReflogStorage) Reference(ctx context.Context, name plumbing.ReferenceName) (*plumbing.Reference, error) {
	return s.sto.Reference(ctx, name)
}

func (s noReflogStorage) IterReferences(ctx context.Context) (storer.ReferenceIter, error) {
	return s.sto.IterReferences(ctx)
}

func (s noReflogStorage) CountLooseRefs(ctx context.Context) (int, error) {
	return s.sto.CountLooseRefs(ctx)
}

func (s noReflogStorage) PackRefs(ctx context.Context) error {
	return s.sto.PackRefs(ctx)
}

func (s noReflogStorage) RemoveReference(ctx context.Context, name plumbing.ReferenceName) error {
	return s.sto.RemoveReference(ctx, name)
}

func (s noReflogStorage) SetIndex(ctx context.Context, idx *index.Index) error {
	return s.sto.SetIndex(ctx, idx)
}

func (s noReflogStorage) Index(ctx context.Context) (*index.Index, error) {
	return s.sto.Index(ctx)
}

func (s noReflogStorage) SetShallow(ctx context.Context, commits []plumbing.Hash) error {
	return s.sto.SetShallow(ctx, commits)
}

func (s noReflogStorage) Shallow(ctx context.Context) ([]plumbing.Hash, error) {
	return s.sto.Shallow(ctx)
}

func (s noReflogStorage) SetConfig(ctx context.Context, cfg *config.Config) error {
	return s.sto.SetConfig(ctx, cfg)
}

func (s noReflogStorage) Config(ctx context.Context) (*config.Config, error) {
	return s.sto.Config(ctx)
}

func (s noReflogStorage) Module(ctx context.Context, name string) (storage.Storer, error) {
	return s.sto.Module(ctx, name)
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
	s.NoError(base.AppendReflog(s.T().Context(), testRef, baseEntry))

	tempEntry := newEntry("commit: temporal")
	s.NoError(rs.AppendReflog(s.T().Context(), testRef, tempEntry))

	entries, err := rs.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Len(entries, 2)
	s.Equal("commit: base", entries[0].Message)
	s.Equal("commit: temporal", entries[1].Message)
}

func (s *ReflogSuite) TestReflogBaseOnly() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(s.T().Context(), testRef, newEntry("commit: base")))

	entries, err := rs.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Len(entries, 1)
	s.Equal("commit: base", entries[0].Message)
}

func (s *ReflogSuite) TestReflogTemporalOnly() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(rs.AppendReflog(s.T().Context(), testRef, newEntry("commit: temporal")))

	entries, err := rs.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Len(entries, 1)
	s.Equal("commit: temporal", entries[0].Message)
}

func (s *ReflogSuite) TestReflogEmpty() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	entries, err := rs.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Empty(entries)
}

func (s *ReflogSuite) TestDeleteHidesBase() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(s.T().Context(), testRef, newEntry("commit: base")))
	s.NoError(rs.DeleteReflog(s.T().Context(), testRef))

	entries, err := rs.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Empty(entries)
}

func (s *ReflogSuite) TestDeleteThenAppend() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(s.T().Context(), testRef, newEntry("commit: old")))
	s.NoError(rs.DeleteReflog(s.T().Context(), testRef))
	s.NoError(rs.AppendReflog(s.T().Context(), testRef, newEntry("commit: new")))

	entries, err := rs.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Len(entries, 1)
	s.Equal("commit: new", entries[0].Message)
}

func (s *ReflogSuite) TestCommitFlushesAppends() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(s.T().Context(), testRef, newEntry("commit: base")))
	s.NoError(rs.AppendReflog(s.T().Context(), testRef, newEntry("commit: temporal")))
	s.NoError(rs.Commit(s.T().Context()))

	// Base should now have both entries.
	entries, err := base.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Len(entries, 2)
	s.Equal("commit: base", entries[0].Message)
	s.Equal("commit: temporal", entries[1].Message)
}

func (s *ReflogSuite) TestCommitFlushesDeletes() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(s.T().Context(), testRef, newEntry("commit: base")))
	s.NoError(rs.DeleteReflog(s.T().Context(), testRef))
	s.NoError(rs.Commit(s.T().Context()))

	// Base should be empty for this ref.
	entries, err := base.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Empty(entries)
}

func (s *ReflogSuite) TestCommitDeleteThenAppend() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(base.AppendReflog(s.T().Context(), testRef, newEntry("commit: old")))
	s.NoError(rs.DeleteReflog(s.T().Context(), testRef))
	s.NoError(rs.AppendReflog(s.T().Context(), testRef, newEntry("commit: new")))
	s.NoError(rs.Commit(s.T().Context()))

	// Base should have only the new entry (old was deleted).
	entries, err := base.Reflog(s.T().Context(), testRef)
	s.NoError(err)
	s.Len(entries, 1)
	s.Equal("commit: new", entries[0].Message)
}

func (s *ReflogSuite) TestBaseUntouchedBeforeCommit() {
	base := memory.NewStorage()
	temporal := memory.NewStorage()
	rs := NewReflogStorage(base, temporal)

	s.NoError(rs.AppendReflog(s.T().Context(), testRef, newEntry("commit: temporal")))

	// Base should be unaffected before Commit.
	entries, err := base.Reflog(s.T().Context(), testRef)
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
		Reflog(context.Context, plumbing.ReferenceName) ([]*reflog.Entry, error)
	})
	s.False(ok)
}
