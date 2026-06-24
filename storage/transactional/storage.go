package transactional

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// Storage is a transactional implementation of git.Storer, it demux the write
// and read operation of two separate storers, allowing to merge content calling
// Storage.Commit.
//
// The API and functionality of this package are considered EXPERIMENTAL and is
// not considered stable nor production ready.
type Storage interface {
	storage.Storer
	Commit() error
}

// basic implements the Storage interface.
type basic struct {
	s, temporal storage.Storer

	*ObjectStorage
	*ReferenceStorage
	*IndexStorage
	*ShallowStorage
	*ConfigStorage
	reflog *ReflogStorage
}

// packageWriter implements storer.PackfileWriter interface over
// a Storage with a temporal storer that supports it.
type packageWriter struct {
	*basic
	pw storer.PackfileWriter
}

type reflogBasic struct {
	*basic
}

type reflogPackageWriter struct {
	*reflogBasic
	pw storer.PackfileWriter
}

// NewStorage returns a new Storage based on two repositories, base is the base
// repository where the read operations are read and temporal is were all
// the write operations are stored.
func NewStorage(base, temporal storage.Storer) Storage {
	st := &basic{
		s:        base,
		temporal: temporal,

		ObjectStorage:    NewObjectStorage(base, temporal),
		ReferenceStorage: NewReferenceStorage(base, temporal),
		IndexStorage:     NewIndexStorage(base, temporal),
		ShallowStorage:   NewShallowStorage(base, temporal),
		ConfigStorage:    NewConfigStorage(base, temporal),
	}

	if baseReflog, ok := base.(storer.ReflogStorer); ok {
		if tempReflog, ok := temporal.(storer.ReflogStorer); ok {
			st.reflog = NewReflogStorage(baseReflog, tempReflog)
		}
	}

	pw, ok := temporal.(storer.PackfileWriter)
	if ok {
		if st.reflog != nil {
			return &reflogPackageWriter{
				reflogBasic: &reflogBasic{basic: st},
				pw:          pw,
			}
		}

		return &packageWriter{
			basic: st,
			pw:    pw,
		}
	}

	if st.reflog != nil {
		return &reflogBasic{basic: st}
	}

	return st
}

// Module it honors the storage.ModuleStorer interface.
func (s *basic) Module(name string) (storage.Storer, error) {
	base, err := s.s.Module(name)
	if err != nil {
		return nil, err
	}

	temporal, err := s.temporal.Module(name)
	if err != nil {
		return nil, err
	}

	return NewStorage(base, temporal), nil
}

// Commit it copies the content of the temporal storage into the base storage.
func (s *basic) Commit() error {
	for _, c := range []interface{ Commit() error }{
		s.ObjectStorage,
		s.ReferenceStorage,
		s.IndexStorage,
		s.ShallowStorage,
		s.ConfigStorage,
	} {
		if err := c.Commit(); err != nil {
			return err
		}
	}

	if s.reflog != nil {
		if err := s.reflog.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// Close closes both the base and temporal storages if they implement io.Closer.
func (s *basic) Close() error {
	var err error
	if closer, ok := s.temporal.(io.Closer); ok {
		err = closer.Close()
	}
	if closer, ok := s.s.(io.Closer); ok {
		if closeErr := closer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

// PackfileWriter honors storage.PackfileWriter.
func (s *packageWriter) PackfileWriter() (io.WriteCloser, error) {
	return s.pw.PackfileWriter()
}

func (s *reflogBasic) Reflog(name plumbing.ReferenceName) ([]*reflog.Entry, error) {
	return s.reflog.Reflog(name)
}

func (s *reflogBasic) AppendReflog(name plumbing.ReferenceName, entry *reflog.Entry) error {
	return s.reflog.AppendReflog(name, entry)
}

func (s *reflogBasic) DeleteReflog(name plumbing.ReferenceName) error {
	return s.reflog.DeleteReflog(name)
}

// PackfileWriter honors storer.PackfileWriter.
func (s *reflogPackageWriter) PackfileWriter() (io.WriteCloser, error) {
	return s.pw.PackfileWriter()
}
