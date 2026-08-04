package filesystem

import (
	"bufio"
	"errors"
	"hash"
	"os"
	"time"

	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/trace"
)

// IndexStorage implements index read/write backed by the filesystem.
type IndexStorage struct {
	dir      *dotgit.DotGit
	h        hash.Hash
	cache    IndexCache
	skipHash bool
	// version holds index.version as configured, and is used when no index
	// file exists yet. An existing index keeps the version recorded in its
	// header, matching git, which only consults index.version when it has
	// no version to preserve.
	version uint32
}

// newIndex returns an empty index using the configured format version.
func (s *IndexStorage) newIndex() *index.Index {
	return &index.Index{Version: indexVersion(s.version)}
}

// indexVersion returns the index format version to write for the configured
// index.version value, falling back to [index.DefaultVersion] when it is unset
// or names a version that cannot be encoded. This mirrors git's
// get_index_format_default, which warns and uses INDEX_FORMAT_DEFAULT rather
// than failing when index.version is out of range.
func indexVersion(v uint32) uint32 {
	if v < index.DecodeVersionSupported.Min || v > index.EncodeVersionSupported {
		return index.DefaultVersion
	}

	return v
}

// SetIndex writes the index to disk and updates the cache.
func (s *IndexStorage) SetIndex(idx *index.Index) (err error) {
	if err := s.writeIndex(idx); err != nil {
		return err
	}

	if s.cache != nil {
		fi, statErr := s.dir.StatIndex()
		if statErr == nil {
			cp := copyIndex(idx)
			cp.ModTime = fi.ModTime()
			s.cache.Set(cp, fi.ModTime(), fi.Size())
		} else {
			s.cache.Clear()
		}
	}

	return nil
}

func (s *IndexStorage) writeIndex(idx *index.Index) (err error) {
	f, err := s.dir.IndexWriter()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)
	bw := bufio.NewWriter(f)
	defer func() {
		if e := bw.Flush(); err == nil && e != nil {
			err = e
		}
	}()

	var encOpts []index.Option
	if s.skipHash {
		encOpts = append(encOpts, index.WithSkipHash())
	}

	e := index.NewEncoder(bw, s.h, encOpts...)
	return e.Encode(idx)
}

// Index reads the index from disk, using the cache when available.
func (s *IndexStorage) Index() (i *index.Index, err error) {
	if trace.Performance.Enabled() {
		start := time.Now()
		defer func() {
			trace.Performance.Printf("performance: %.9f s: storage/filesystem: get index()", time.Since(start).Seconds())
		}()
	}

	if s.cache != nil {
		fi, err := s.dir.StatIndex()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				s.cache.Clear()
				return s.newIndex(), nil
			}
			return nil, err
		}

		if cached := s.cache.Get(fi.ModTime(), fi.Size()); cached != nil {
			return copyIndex(cached), nil
		}
	}

	idx := s.newIndex()

	f, err := s.dir.Index()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return idx, nil
		}
		return nil, err
	}

	defer ioutil.CheckClose(f, &err)

	fi, err := f.Stat()
	var sz int64
	if err == nil {
		idx.ModTime = fi.ModTime()
		sz = fi.Size()
	}

	var decOpts []index.Option
	if s.skipHash {
		decOpts = append(decOpts, index.WithSkipHash())
	}

	d := index.NewDecoder(f, s.h, decOpts...)
	err = d.Decode(idx)
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		s.cache.Set(idx, idx.ModTime, sz)
	}

	return copyIndex(idx), nil
}

// copyIndex returns a shallow copy of the Index struct with its own
// copy of the Entries slice, so that callers can append/remove entries
// without affecting the cached copy. Individual *Entry pointers are
// shared; this is safe because callers replace entries rather than
// mutating them in place.
func copyIndex(idx *index.Index) *index.Index {
	cp := *idx
	cp.Entries = make([]*index.Entry, len(idx.Entries))
	copy(cp.Entries, idx.Entries)
	return &cp
}
