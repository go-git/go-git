// Package dotgit implements the .git directory layout.
// https://github.com/git/git/blob/master/Documentation/gitrepository-layout.txt
package dotgit

import (
	"bufio"
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"hash"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/packhandle"
	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/format/revfile"
	plumbhash "github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/x/fdpool"
)

const (
	packedRefsPath     = "packed-refs"
	configPath         = "config"
	configWorktreePath = "config.worktree"
	indexPath          = "index"
	shallowPath        = "shallow"
	modulePath         = "modules"
	objectsPath        = "objects"
	packPath           = "pack"
	refsPath           = "refs"
	branchesPath       = "branches"
	hooksPath          = "hooks"
	infoPath           = "info"
	remotesPath        = "remotes"
	logsPath           = "logs"
	worktreesPath      = "worktrees"
	alternatesPath     = "alternates"

	tmpPackedRefsPrefix = "._packed-refs"

	packPrefix = "pack-"
	packExt    = ".pack"
)

var (
	// ErrNotFound is returned by New when the path is not found.
	ErrNotFound = errors.New("path not found")
	// ErrIdxNotFound is returned by Idxfile when the idx file is not found
	ErrIdxNotFound = errors.New("idx file not found")
	// ErrPackfileNotFound is returned by Packfile when the packfile is not found
	ErrPackfileNotFound = errors.New("packfile not found")
	// ErrConfigNotFound is returned by Config when the config is not found
	ErrConfigNotFound = errors.New("config file not found")
	// ErrPackedRefsDuplicatedRef is returned when a duplicated reference is
	// found in the packed-ref file. This is usually the case for corrupted git
	// repositories.
	ErrPackedRefsDuplicatedRef = errors.New("duplicated ref found in packed-ref file")
	// ErrPackedRefsBadFormat is returned when the packed-ref file corrupt.
	ErrPackedRefsBadFormat = errors.New("malformed packed-ref")
	// ErrSymRefTargetNotFound is returned when a symbolic reference is
	// targeting a non-existing object. This usually means the repository
	// is corrupt.
	ErrSymRefTargetNotFound = errors.New("symbolic reference target not found")
	// ErrIsDir is returned when a reference file is attempting to be read,
	// but the path specified is a directory.
	ErrIsDir = errors.New("reference path is a directory")
	// ErrEmptyRefFile is returned when a reference file is attempted to be read,
	// but the file is empty
	ErrEmptyRefFile = errors.New("ref file is empty")
	// ErrModuleNameEscape is returned when a submodule name would
	// resolve outside the modules/ subtree, mirroring canonical Git's
	// "ignoring suspicious submodule name" defence.
	ErrModuleNameEscape = errors.New("submodule name escapes modules/ directory")
	// ErrReferenceNameEscape is returned when a reference name would
	// resolve outside its reference sub-tree once turned into a path
	// under the .git directory (e.g. a name with a ".." component).
	ErrReferenceNameEscape = errors.New("reference name escapes the reference storage")
)

func isPathSep(r rune) bool { return r == '/' || r == '\\' }

// validReferenceName rejects reference names that cannot be safely turned into
// a path under the .git directory. A loose reference (and its reflog) is stored
// verbatim at ".git/<name>", so a crafted name — for instance one advertised by
// a malicious remote — could climb out of its reference sub-tree and read,
// overwrite, or delete unrelated metadata such as .git/config.
//
// The storage-safety gate is plumbing.ReferenceName.IsSafe, mirroring Git's
// refname_is_safe: a name must be under refs/ without escaping it, or be a
// [A-Z_] pseudo-ref. This alone rejects absolute, drive-prefixed, escaping and
// single-level metadata names. On top of it, this adds filesystem-specific
// hardening that IsSafe's literal check does not cover: control characters, and
// components a case-insensitive/NTFS/HFS+ filesystem would fold back to "." or
// ".." (trailing dots/spaces, Alternate Data Streams, ignorable Unicode code
// points). The per-component check is delegated to pathutil.IsHFSDot and
// pathutil.IsNTFSDot with "." as the needle, exactly as validSubmoduleName
// does, and runs regardless of host OS because a name can be authored on one OS
// and reach this layer on another.
func validReferenceName(name plumbing.ReferenceName) error {
	if !name.IsSafe() {
		return fmt.Errorf("%w: %q is not under refs/ nor a valid pseudo-ref", ErrReferenceNameEscape, string(name))
	}

	s := string(name)
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return fmt.Errorf("%w: %q", ErrReferenceNameEscape, s)
		}
	}
	for _, part := range strings.FieldsFunc(s, isPathSep) {
		// IsNTFSDot/IsHFSDot with a "." needle match ".." and its disguises
		// but not a bare ".", so reject that component explicitly too.
		if part == "." || pathutil.IsHFSDot(part, ".") || pathutil.IsNTFSDot(part, ".", "") {
			return fmt.Errorf("%w: %q", ErrReferenceNameEscape, s)
		}
	}
	return nil
}

// Options holds configuration for the storage.
type Options struct {
	// ExclusiveAccess means that the filesystem is not modified externally
	// while the repo is open.
	ExclusiveAccess bool
	// AlternatesFS provides the billy filesystem to be used for Git Alternates.
	// If none is provided, it falls back to using the underlying instance used for
	// DotGit.
	AlternatesFS billy.Filesystem

	ObjectFormat formatcfg.ObjectFormat

	// ReadReverseIndex controls whether .rev files are read from disk.
	// When false, a reverse index is generated in memory on demand.
	// Defaults to true.
	ReadReverseIndex bool
	// WriteReverseIndex controls whether .rev files are written when
	// creating new packfiles. Defaults to true.
	WriteReverseIndex bool
	// Pool, when non-nil, governs the LRU eviction of file
	// descriptors held by [packhandle.PackHandle] instances
	// constructed by this DotGit. Nil disables pooling for the
	// pack-FD lifecycle (grace-period close on quiescence).
	//
	// The Pool field's API stability tracks [fdpool.Pool]'s, not
	// this package's. Per the x/ package policy, the fdpool API
	// may change without following semantic versioning; consumers
	// reading this field should treat it as experimental on the
	// same timeline.
	Pool *fdpool.Pool
}

// The DotGit type represents a local git repository on disk. This
// type is not zero-value-safe, use the New function to initialize it.
type DotGit struct {
	options Options
	fs      billy.Filesystem

	// incoming object directory information. incomingDirName is written
	// exactly once inside incomingOnce.Do; sync.Once provides the
	// happens-before guarantee that lets readers observe it lock-free.
	incomingOnce    sync.Once
	incomingDirName string

	objectList []plumbing.Hash // sorted
	objectMap  map[plumbing.Hash]struct{}
	packList   []plumbing.Hash
	packMap    map[plumbing.Hash]struct{}

	// packHandles caches one [*packhandle.PackHandle] per pack hash,
	// guarded by packHandlesMu. Lazy-initialised on first use.
	packHandlesMu sync.Mutex
	packHandles   map[plumbing.Hash]*packhandle.PackHandle
}

// New returns a DotGit value ready to be used. The path argument must
// be the absolute path of a git repository directory (e.g.
// "/foo/bar/.git").
func New(fs billy.Filesystem) *DotGit {
	return NewWithOptions(fs, Options{
		ObjectFormat:      formatcfg.DefaultObjectFormat,
		ReadReverseIndex:  true,
		WriteReverseIndex: true,
	})
}

// NewWithOptions sets non default configuration options.
// See New for complete help.
func NewWithOptions(fs billy.Filesystem, o Options) *DotGit {
	return &DotGit{
		options: o,
		fs:      fs,
	}
}

// Initialize creates all the folder scaffolding.
func (d *DotGit) Initialize() error {
	mustExists := []string{
		d.fs.Join("objects", "info"),
		d.fs.Join("objects", "pack"),
		d.fs.Join("refs", "heads"),
		d.fs.Join("refs", "tags"),
	}

	for _, path := range mustExists {
		_, err := d.fs.Stat(path)
		if err == nil {
			continue
		}

		if !os.IsNotExist(err) {
			return err
		}

		if err := d.fs.MkdirAll(path, os.ModeDir|os.ModePerm); err != nil {
			return err
		}
	}

	return nil
}

// Close closes all opened files.
func (d *DotGit) Close() error {
	d.packHandlesMu.Lock()
	handles := d.packHandles
	d.packHandles = nil
	d.packHandlesMu.Unlock()

	var phErrs []error
	for _, h := range handles {
		if err := h.Close(); err != nil {
			phErrs = append(phErrs, err)
		}
	}

	d.packMap = nil

	return errors.Join(phErrs...)
}

// ConfigWriter returns a file pointer for write to the config file
func (d *DotGit) ConfigWriter() (billy.File, error) {
	return d.fs.Create(configPath)
}

// Config returns a file pointer for read to the config file
func (d *DotGit) Config() (billy.File, error) {
	return d.fs.Open(configPath)
}

// ConfigWorktree returns a file pointer for read to the worktree config file.
func (d *DotGit) ConfigWorktree() (billy.File, error) {
	return d.fs.Open(configWorktreePath)
}

// ConfigWorktreeWriter returns a file pointer for write to the worktree config file.
func (d *DotGit) ConfigWorktreeWriter() (billy.File, error) {
	return d.fs.Create(configWorktreePath)
}

// IndexWriter returns a file pointer for write to the index file
func (d *DotGit) IndexWriter() (billy.File, error) {
	return d.fs.Create(indexPath)
}

// Index returns a file pointer for read to the index file
func (d *DotGit) Index() (billy.File, error) {
	return d.fs.Open(indexPath)
}

// StatIndex returns the os.FileInfo for the index file without opening it.
func (d *DotGit) StatIndex() (os.FileInfo, error) {
	return d.fs.Stat(indexPath)
}

// ShallowWriter returns a file pointer for write to the shallow file
func (d *DotGit) ShallowWriter() (billy.File, error) {
	return d.fs.Create(shallowPath)
}

// Shallow returns a file pointer for read to the shallow file
func (d *DotGit) Shallow() (billy.File, error) {
	f, err := d.fs.Open(shallowPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	return f, nil
}

// ReflogReader returns a file pointer for reading the reflog for the given reference.
// Returns nil, nil if the reflog file does not exist.
func (d *DotGit) ReflogReader(name plumbing.ReferenceName) (billy.File, error) {
	if err := validReferenceName(name); err != nil {
		return nil, err
	}
	p := d.fs.Join(logsPath, string(name))
	f, err := d.fs.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}

// ReflogWriter returns a file pointer for appending to the reflog for the given reference.
// It creates the file and any necessary parent directories if they don't exist.
func (d *DotGit) ReflogWriter(name plumbing.ReferenceName) (billy.File, error) {
	if err := validReferenceName(name); err != nil {
		return nil, err
	}
	p := d.fs.Join(logsPath, string(name))
	if err := d.fs.MkdirAll(filepath.Dir(p), os.ModePerm); err != nil {
		return nil, err
	}
	return d.fs.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
}

// DeleteReflog removes the reflog file for the given reference.
func (d *DotGit) DeleteReflog(name plumbing.ReferenceName) error {
	if err := validReferenceName(name); err != nil {
		return err
	}
	p := d.fs.Join(logsPath, string(name))
	err := d.fs.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// NewObjectPack return a writer for a new packfile, it saves the packfile to
// disk and also generates and save the index for the given packfile.
func (d *DotGit) NewObjectPack() (*PackWriter, error) {
	cleanErr := d.cleanPackList()
	pw, err := newPackWrite(d.fs, d.options.ObjectFormat, d.options.WriteReverseIndex)
	if err != nil {
		return nil, errors.Join(cleanErr, err)
	}
	if cleanErr != nil {
		return nil, cleanErr
	}
	return pw, nil
}

// ObjectPacks returns the list of availables packfiles
func (d *DotGit) ObjectPacks() ([]plumbing.Hash, error) {
	if !d.options.ExclusiveAccess {
		return d.objectPacks()
	}

	err := d.genPackList()
	if err != nil {
		return nil, err
	}

	return d.packList, nil
}

func (d *DotGit) objectPacks() ([]plumbing.Hash, error) {
	packDir := d.fs.Join(objectsPath, packPath)
	files, err := d.fs.ReadDir(packDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	packs := make([]plumbing.Hash, 0, len(files))
	for _, f := range files {
		n := f.Name()
		if !strings.HasSuffix(n, packExt) || !strings.HasPrefix(n, packPrefix) {
			continue
		}

		h := plumbing.NewHash(n[5 : len(n)-5]) // pack-(hash).pack
		if h.IsZero() {
			// Ignore files with badly-formatted names.
			continue
		}
		packs = append(packs, h)
	}

	return packs, nil
}

func (d *DotGit) objectPackPath(hash plumbing.Hash, extension string) string {
	return d.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s.%s", hash.String(), extension))
}

func (d *DotGit) objectPackOpen(hash plumbing.Hash, extension string) (billy.File, error) {
	err := d.hasPack(hash)
	if err != nil {
		return nil, err
	}

	path := d.objectPackPath(hash, extension)
	pack, err := d.fs.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPackfileNotFound
		}

		return nil, err
	}

	return pack, nil
}

// ObjectPack returns a fs.File of the given packfile.
func (d *DotGit) ObjectPack(hash plumbing.Hash) (billy.File, error) {
	err := d.hasPack(hash)
	if err != nil {
		return nil, err
	}

	return d.objectPackOpen(hash, `pack`)
}

// ObjectPackIdx returns a fs.File of the index file for a given packfile.
func (d *DotGit) ObjectPackIdx(hash plumbing.Hash) (billy.File, error) {
	err := d.hasPack(hash)
	if err != nil {
		return nil, err
	}

	return d.objectPackOpen(hash, `idx`)
}

// ObjectPackRev returns a fs.File of the reverse index file for a given packfile.
func (d *DotGit) ObjectPackRev(hash plumbing.Hash) (billy.File, error) {
	err := d.hasPack(hash)
	if err != nil {
		return nil, err
	}

	return d.objectPackOpen(hash, `rev`)
}

// OpenPackRev returns a [idxfile.ReadAtCloser] for the reverse index of the given
// packfile. When ReadReverseIndex is true the .rev file is read from disk;
// otherwise the reverse index is generated in memory on demand.
func (d *DotGit) OpenPackRev(hash plumbing.Hash) (idxfile.ReadAtCloser, error) {
	if d.options.ReadReverseIndex {
		r, err := d.ObjectPackRev(hash)
		if err == nil {
			return r, nil
		}
	}
	return d.generateInMemoryRev(hash)
}

// generateInMemoryRev builds the reverse index data in memory from the
// .idx file. A fresh reverse index is generated on every call.
func (d *DotGit) generateInMemoryRev(h plumbing.Hash) (idxfile.ReadAtCloser, error) {
	f, err := d.ObjectPackIdx(h)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	var hasher hash.Hash
	if h.Size() == crypto.SHA256.Size() {
		hasher = plumbhash.New(crypto.SHA256)
	} else {
		hasher = plumbhash.New(crypto.SHA1)
	}

	idx := idxfile.NewMemoryIndex(h.Size())
	dec := idxfile.NewDecoder(f, hasher)
	if err := dec.Decode(idx); err != nil {
		return nil, fmt.Errorf("cannot decode idx for in-memory rev generation: %w", err)
	}

	hasher.Reset()

	var buf bytes.Buffer
	if err := revfile.Encode(&buf, hasher, idx); err != nil {
		return nil, fmt.Errorf("cannot encode in-memory rev: %w", err)
	}

	return newBytesReadAtCloser(buf.Bytes()), nil
}

// bytesReadAtCloser wraps a bytes.Reader to satisfy [idxfile.ReadAtCloser].
type bytesReadAtCloser struct {
	*bytes.Reader
}

func newBytesReadAtCloser(data []byte) *bytesReadAtCloser {
	return &bytesReadAtCloser{Reader: bytes.NewReader(data)}
}

func (b *bytesReadAtCloser) Close() error { return nil }

// packHandle returns the cached [packhandle.PackHandle] for the
// given pack hash, constructing one on first access. Rev is
// served through [DotGit.OpenPackRev] so the in-memory fallback
// applies when the .rev file is absent or [Options.ReadReverseIndex]
// is false. Returns [ErrPackfileNotFound] when the .pack file
// cannot be located.
func (d *DotGit) packHandle(hash plumbing.Hash) (*packhandle.PackHandle, error) {
	d.packHandlesMu.Lock()
	defer d.packHandlesMu.Unlock()

	if h, ok := d.packHandles[hash]; ok {
		return h, nil
	}

	if err := d.hasPack(hash); err != nil {
		return nil, err
	}

	packPath := d.objectPackPath(hash, "pack")
	idxPath := d.objectPackPath(hash, "idx")

	sources := packhandle.Sources{
		Pack: packhandle.Source{
			Open: func() (packhandle.ReadAtCloser, error) {
				return d.ObjectPack(hash)
			},
			Size: func() (int64, error) {
				fi, err := d.fs.Stat(packPath)
				if err != nil {
					return 0, err
				}
				return fi.Size(), nil
			},
		},
		Idx: packhandle.Source{
			Open: func() (packhandle.ReadAtCloser, error) {
				return d.ObjectPackIdx(hash)
			},
			Size: func() (int64, error) {
				fi, err := d.fs.Stat(idxPath)
				if err != nil {
					return 0, err
				}
				return fi.Size(), nil
			},
		},
		Rev: packhandle.Source{
			Open: func() (packhandle.ReadAtCloser, error) {
				return d.OpenPackRev(hash)
			},
			// Rev may be served from an in-memory buffer with no
			// stat-able size. [idxfile.LazyIndex] does not consult
			// Rev.Size, so a zero stub is correct here.
			Size: func() (int64, error) { return 0, nil },
		},
	}

	ph, err := packhandle.NewWithPool(sources, hash, d.options.Pool)
	if err != nil {
		return nil, err
	}

	if d.packHandles == nil {
		d.packHandles = make(map[plumbing.Hash]*packhandle.PackHandle)
	}
	d.packHandles[hash] = ph
	return ph, nil
}

// walkPackHandles snapshots the [packhandle.PackHandle] catalog
// under packHandlesMu and invokes fn on each entry with the lock
// released. The snapshot shortens the critical section so
// concurrent [DotGit.packHandle] lookups do not block on slow
// per-handle work such as file-close syscalls.
//
// The catalog is left intact; fn observes the same PackHandle
// pointers that subsequent lookups return. Callers that want to
// drain the catalog must do so separately — Close and
// cleanPackList keep their inline snapshot-with-clear because
// they need the drain to be atomic with the snapshot.
//
// Errors returned by fn are joined; the walk continues past a
// failing entry so one bad pack does not strand FDs in others.
func (d *DotGit) walkPackHandles(fn func(*packhandle.PackHandle) error) error {
	d.packHandlesMu.Lock()
	handles := slices.Collect(maps.Values(d.packHandles))
	d.packHandlesMu.Unlock()

	var errs []error
	for _, h := range handles {
		if err := fn(h); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CloseIdleDescriptors releases the FDs held by every cached
// [packhandle.PackHandle] without evicting them from the
// catalog. The catalog and each PackHandle's cached state
// (`PackMeta`, `LazyIndex`) survive the call; subsequent
// operations reopen FDs on demand.
//
// Idempotent and safe to call concurrently with other DotGit
// operations. After [DotGit.Close] the catalog is empty, so the
// call is a no-op.
func (d *DotGit) CloseIdleDescriptors() error {
	return d.walkPackHandles(func(ph *packhandle.PackHandle) error {
		return ph.CloseIdleDescriptors()
	})
}

// PackHandle returns the cached [packfile.PackHandle] for the
// given pack hash, constructing one on first access. The returned
// interface does not expose Close; dotgit owns the handle's
// lifetime and tears it down via [DotGit.Close] or
// [DotGit.cleanPackList].
func (d *DotGit) PackHandle(hash plumbing.Hash) (packfile.PackHandle, error) {
	ph, err := d.packHandle(hash)
	if err != nil {
		return nil, err
	}
	return packHandleAdapter{ph}, nil
}

// packHandleAdapter satisfies [packfile.PackHandle] by delegating
// to the dotgit-owned [packhandle.PackHandle]. Unexported so
// callers cannot type-assert back to the concrete type and reach
// [packhandle.PackHandle.Close].
type packHandleAdapter struct{ ph *packhandle.PackHandle }

func (a packHandleAdapter) OpenPackReader() (io.ReadSeekCloser, error) {
	return a.ph.OpenPackReader()
}

func (a packHandleAdapter) OpenRandomReader() (packfile.RandomReader, error) {
	return a.ph.OpenRandomReader()
}

func (a packHandleAdapter) PackHash() (plumbing.Hash, error) {
	m, err := a.ph.Meta()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return m.ID, nil
}

// DeleteOldObjectPackAndIndex removes a pack and its index if older than t.
// The .pack, .idx and .rev files are each attempted independently; any
// failures are joined into the returned error so a partial failure cannot
// leave orphaned siblings on disk. A missing .rev is not an error — the
// reverse index is optional and may have been generated only in memory.
func (d *DotGit) DeleteOldObjectPackAndIndex(hash plumbing.Hash, t time.Time) error {
	var errs []error
	if err := d.cleanPackList(); err != nil {
		errs = append(errs, err)
	}

	packPath := d.objectPackPath(hash, `pack`)
	if !t.IsZero() {
		fi, err := d.fs.Stat(packPath)
		if err != nil {
			errs = append(errs, err)
			return errors.Join(errs...)
		}
		// too new, skip deletion.
		if !fi.ModTime().Before(t) {
			return errors.Join(errs...)
		}
	}

	for _, ext := range []string{`pack`, `idx`, `rev`} {
		if err := d.fs.Remove(d.objectPackPath(hash, ext)); err != nil {
			if ext == `rev` && os.IsNotExist(err) {
				continue
			}
			errs = append(errs, err)
		}
	}

	d.packHandlesMu.Lock()
	ph, ok := d.packHandles[hash]
	if ok {
		delete(d.packHandles, hash)
	}
	d.packHandlesMu.Unlock()
	if ok {
		if err := ph.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NewObject return a writer for a new object file.
func (d *DotGit) NewObject() (*ObjectWriter, error) {
	d.cleanObjectList()

	return newObjectWriter(d.fs, d.options.ObjectFormat)
}

// ObjectsWithPrefix returns the hashes of objects that have the given prefix.
func (d *DotGit) ObjectsWithPrefix(prefix []byte) ([]plumbing.Hash, error) {
	// Handle edge cases.
	if len(prefix) < 1 {
		return d.Objects()
	} else if len(prefix) > plumbing.ZeroHash.Size() {
		return nil, nil
	}

	if d.options.ExclusiveAccess {
		err := d.genObjectList()
		if err != nil {
			return nil, err
		}

		// Rely on d.objectList being sorted.
		// Figure out the half-open interval defined by the prefix.
		first := sort.Search(len(d.objectList), func(i int) bool {
			// Same as plumbing.HashSlice.Less.
			return bytes.Compare(d.objectList[i].Bytes(), prefix) >= 0
		})
		lim := len(d.objectList)
		if limPrefix, overflow := incBytes(prefix); !overflow {
			lim = sort.Search(len(d.objectList), func(i int) bool {
				// Same as plumbing.HashSlice.Less.
				return bytes.Compare(d.objectList[i].Bytes(), limPrefix) >= 0
			})
		}
		return d.objectList[first:lim], nil
	}

	// This is the slow path.
	var objects []plumbing.Hash
	var n int
	err := d.ForEachObjectHash(func(hash plumbing.Hash) error {
		n++
		if bytes.HasPrefix(hash.Bytes(), prefix) {
			objects = append(objects, hash)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return objects, nil
}

// Objects returns a slice with the hashes of objects found under the
// .git/objects/ directory.
func (d *DotGit) Objects() ([]plumbing.Hash, error) {
	if d.options.ExclusiveAccess {
		err := d.genObjectList()
		if err != nil {
			return nil, err
		}

		return d.objectList, nil
	}

	var objects []plumbing.Hash
	err := d.ForEachObjectHash(func(hash plumbing.Hash) error {
		objects = append(objects, hash)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return objects, nil
}

// ForEachObjectHash iterates over the hashes of objects found under the
// .git/objects/ directory and executes the provided function.
func (d *DotGit) ForEachObjectHash(fun func(plumbing.Hash) error) error {
	if !d.options.ExclusiveAccess {
		return d.forEachObjectHash(fun)
	}

	err := d.genObjectList()
	if err != nil {
		return err
	}

	for _, h := range d.objectList {
		err := fun(h)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *DotGit) forEachObjectHash(fun func(plumbing.Hash) error) error {
	files, err := d.fs.ReadDir(objectsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	for _, f := range files {
		if f.IsDir() && len(f.Name()) == 2 && isHex(f.Name()) {
			base := f.Name()
			d, err := d.fs.ReadDir(d.fs.Join(objectsPath, base))
			if err != nil {
				return err
			}

			for _, o := range d {
				h := plumbing.NewHash(base + o.Name())
				if h.IsZero() {
					// Ignore files with badly-formatted names.
					continue
				}
				err = fun(h)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (d *DotGit) cleanObjectList() {
	d.objectMap = nil
	d.objectList = nil
}

func (d *DotGit) genObjectList() error {
	if d.objectMap != nil {
		return nil
	}

	d.objectMap = make(map[plumbing.Hash]struct{})
	populate := func(h plumbing.Hash) error {
		d.objectList = append(d.objectList, h)
		d.objectMap[h] = struct{}{}

		return nil
	}
	if err := d.forEachObjectHash(populate); err != nil {
		return err
	}
	plumbing.HashesSort(d.objectList)
	return nil
}

func (d *DotGit) hasObject(h plumbing.Hash) error {
	if !d.options.ExclusiveAccess {
		return nil
	}

	err := d.genObjectList()
	if err != nil {
		return err
	}

	_, ok := d.objectMap[h]
	if !ok {
		return plumbing.ErrObjectNotFound
	}

	return nil
}

// cleanPackList resets the pack catalog and drops every cached
// [packhandle.PackHandle]. A pack-set mutation may have
// invalidated the handles' underlying files, so they cannot
// safely be reused. Idle readers finish normally; their cursor
// Close becomes a no-op release.
//
// PackHandle.Close owns the underlying sharedfile and LazyIndex
// FDs, so a close failure here means an FD has not been released.
// The errors are joined and returned so callers can surface them
// rather than silently masking I/O failures during cleanup.
func (d *DotGit) cleanPackList() error {
	d.packMap = nil
	d.packList = nil

	d.packHandlesMu.Lock()
	handles := d.packHandles
	d.packHandles = nil
	d.packHandlesMu.Unlock()

	var errs []error
	for _, h := range handles {
		if err := h.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (d *DotGit) genPackList() error {
	if d.packMap != nil {
		return nil
	}

	op, err := d.objectPacks()
	if err != nil {
		return err
	}

	d.packMap = make(map[plumbing.Hash]struct{}, len(op))
	d.packList = nil

	for _, h := range op {
		d.packList = append(d.packList, h)
		d.packMap[h] = struct{}{}
	}

	return nil
}

func (d *DotGit) hasPack(h plumbing.Hash) error {
	if !d.options.ExclusiveAccess {
		return nil
	}

	err := d.genPackList()
	if err != nil {
		return err
	}

	_, ok := d.packMap[h]
	if !ok {
		return ErrPackfileNotFound
	}

	return nil
}

func (d *DotGit) objectPath(h plumbing.Hash) string {
	hex := h.String()
	return d.fs.Join(objectsPath, hex[0:2], hex[2:h.HexSize()])
}

// incomingObjectPath is intended to add support for a git pre-receive hook
// to be written it adds support for go-git to find objects in an "incoming"
// directory, so that the library can be used to write a pre-receive hook
// that deals with the incoming objects.
//
// More on git hooks found here : https://git-scm.com/docs/githooks
// More on 'quarantine'/incoming directory here:
//
//	https://git-scm.com/docs/git-receive-pack
func (d *DotGit) incomingObjectPath(h plumbing.Hash) string {
	hString := h.String()

	if d.incomingDirName == "" {
		return d.fs.Join(objectsPath, hString[0:2], hString[2:h.HexSize()])
	}

	return d.fs.Join(objectsPath, d.incomingDirName, hString[0:2], hString[2:h.HexSize()])
}

// hasIncomingObjects searches for an incoming directory and keeps its name
// so it doesn't have to be found each time an object is accessed. The
// lazy initialisation runs under sync.Once so concurrent callers cannot
// race on the cached fields.
func (d *DotGit) hasIncomingObjects() bool {
	d.incomingOnce.Do(func() {
		directoryContents, err := d.fs.ReadDir(objectsPath)
		if err == nil {
			for _, file := range directoryContents {
				if file.IsDir() && (strings.HasPrefix(file.Name(), "tmp_objdir-incoming-") ||
					// Before Git 2.35 incoming commits directory had another prefix
					strings.HasPrefix(file.Name(), "incoming-")) {
					d.incomingDirName = file.Name()
				}
			}
		}
	})

	return d.incomingDirName != ""
}

// Object returns a fs.File pointing the object file, if exists
func (d *DotGit) Object(h plumbing.Hash) (billy.File, error) {
	err := d.hasObject(h)
	if err != nil {
		return nil, err
	}

	obj1, err1 := d.fs.Open(d.objectPath(h))
	if os.IsNotExist(err1) && d.hasIncomingObjects() {
		obj2, err2 := d.fs.Open(d.incomingObjectPath(h))
		if err2 != nil {
			return obj1, err1
		}
		return obj2, err2
	}
	return obj1, err1
}

// ObjectStat returns a os.FileInfo pointing the object file, if exists
func (d *DotGit) ObjectStat(h plumbing.Hash) (os.FileInfo, error) {
	err := d.hasObject(h)
	if err != nil {
		return nil, err
	}

	obj1, err1 := d.fs.Stat(d.objectPath(h))
	if os.IsNotExist(err1) && d.hasIncomingObjects() {
		obj2, err2 := d.fs.Stat(d.incomingObjectPath(h))
		if err2 != nil {
			return obj1, err1
		}
		return obj2, err2
	}
	return obj1, err1
}

// ObjectDelete removes the object file, if exists
func (d *DotGit) ObjectDelete(h plumbing.Hash) error {
	d.cleanObjectList()

	err1 := d.fs.Remove(d.objectPath(h))
	if os.IsNotExist(err1) && d.hasIncomingObjects() {
		err2 := d.fs.Remove(d.incomingObjectPath(h))
		if err2 != nil {
			return err1
		}
		return err2
	}
	return err1
}

func (d *DotGit) readReferenceFrom(rd io.Reader, name string) (ref *plumbing.Reference, err error) {
	b, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}

	if len(b) == 0 {
		return nil, ErrEmptyRefFile
	}

	line := strings.TrimSpace(string(b))
	return plumbing.NewReferenceFromStrings(name, line), nil
}

// checkReferenceAndTruncate reads the reference from the given file, or the `pack-refs` file if
// the file was empty. Then it checks that the old reference matches the stored reference and
// truncates the file.
func (d *DotGit) checkReferenceAndTruncate(f billy.File, old *plumbing.Reference) error {
	if old == nil {
		return nil
	}

	ref, err := d.readReferenceFrom(f, old.Name().String())
	if errors.Is(err, ErrEmptyRefFile) {
		// This may happen if the reference is being read from a newly created file.
		// In that case, try getting the reference from the packed refs file.
		ref, err = d.packedRef(old.Name())
	}

	if err != nil {
		return err
	}

	if ref.Hash() != old.Hash() {
		return storage.ErrReferenceHasChanged
	}
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	return f.Truncate(0)
}

// SetRef stores a reference, optionally checking that old matches the current value.
func (d *DotGit) SetRef(r, old *plumbing.Reference) error {
	if err := validReferenceName(r.Name()); err != nil {
		return err
	}

	var content string
	switch r.Type() {
	case plumbing.SymbolicReference:
		content = fmt.Sprintf("ref: %s\n", r.Target())
	case plumbing.HashReference:
		content = fmt.Sprintln(r.Hash().String())
	}

	fileName := r.Name().String()

	return d.setRef(fileName, content, old)
}

// Refs scans the git directory collecting references, which it returns.
// Symbolic references are resolved and included in the output.
func (d *DotGit) Refs() ([]*plumbing.Reference, error) {
	var refs []*plumbing.Reference
	seen := make(map[plumbing.ReferenceName]bool)
	if err := d.addRefFromHEAD(&refs); err != nil {
		return nil, err
	}

	if err := d.addRefsFromRefDir(&refs, seen); err != nil {
		return nil, err
	}

	if err := d.addRefsFromPackedRefs(&refs, seen); err != nil {
		return nil, err
	}

	return refs, nil
}

// Ref returns the reference for a given reference name.
func (d *DotGit) Ref(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	if err := validReferenceName(name); err != nil {
		return nil, err
	}

	ref, err := d.readReferenceFile(".", name.String())
	if err == nil {
		return ref, nil
	}

	return d.packedRef(name)
}

func (d *DotGit) findPackedRefsInFile(f billy.File, recv refsRecv) error {
	s := bufio.NewScanner(f)
	for s.Scan() {
		ref, err := d.processLine(s.Text())
		if err != nil {
			return err
		}

		if !recv(ref) {
			// skip parse
			return nil
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	return nil
}

// refsRecv: returning true means that the reference continues to be resolved, otherwise it is stopped, which will speed up the lookup of a single reference.
type refsRecv func(*plumbing.Reference) bool

func (d *DotGit) findPackedRefs(recv refsRecv) error {
	f, err := d.fs.Open(packedRefsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	defer ioutil.CheckClose(f, &err)
	return d.findPackedRefsInFile(f, recv)
}

func (d *DotGit) packedRef(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	var ref *plumbing.Reference
	if err := d.findPackedRefs(func(r *plumbing.Reference) bool {
		if r != nil && r.Name() == name {
			ref = r
			// ref found
			return false
		}
		return true
	}); err != nil {
		return nil, err
	}
	if ref != nil {
		return ref, nil
	}
	return nil, plumbing.ErrReferenceNotFound
}

// RemoveRef removes a reference by name.
func (d *DotGit) RemoveRef(name plumbing.ReferenceName) error {
	if err := validReferenceName(name); err != nil {
		return err
	}

	path := d.fs.Join(".", name.String())
	_, err := d.fs.Stat(path)
	if err == nil {
		err = d.fs.Remove(path)
		// Drop down to remove it from the packed refs file, too.
	}

	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return d.rewritePackedRefsWithoutRef(name)
}

func refsRecvFunc(refs *[]*plumbing.Reference, seen map[plumbing.ReferenceName]bool) refsRecv {
	return func(r *plumbing.Reference) bool {
		if r != nil && !seen[r.Name()] {
			*refs = append(*refs, r)
			seen[r.Name()] = true
		}
		return true
	}
}

func (d *DotGit) addRefsFromPackedRefs(refs *[]*plumbing.Reference, seen map[plumbing.ReferenceName]bool) (err error) {
	return d.findPackedRefs(refsRecvFunc(refs, seen))
}

func (d *DotGit) addRefsFromPackedRefsFile(refs *[]*plumbing.Reference, f billy.File, seen map[plumbing.ReferenceName]bool) (err error) {
	return d.findPackedRefsInFile(f, refsRecvFunc(refs, seen))
}

func (d *DotGit) openAndLockPackedRefs(doCreate bool) (
	pr billy.File, err error,
) {
	var f billy.File
	defer func() {
		if err != nil && f != nil {
			ioutil.CheckClose(f, &err)
		}
	}()

	// File mode is retrieved from a constant defined in the target specific
	// files (dotgit_rewrite_packed_refs_*). Some modes are not available
	// in all filesystems.
	openFlags := d.openAndLockPackedRefsMode()
	if doCreate {
		openFlags |= os.O_CREATE
	}

	start := time.Now()
	// Keep trying to open and lock the file until we're sure the file
	// didn't change between the open and the lock.
	for {
		// The arbitrary timeout should eventually be replaced with
		// context-based check.
		if time.Since(start) > 15*time.Second {
			return nil, errors.New("timeout trying to lock packed refs")
		}
		f, err = d.fs.OpenFile(packedRefsPath, openFlags, 0o600)
		if err != nil {
			if os.IsNotExist(err) && !doCreate {
				return nil, nil
			}

			return nil, err
		}
		fi, err := d.fs.Stat(packedRefsPath)
		if err != nil {
			return nil, err
		}
		mtime := fi.ModTime()

		if locker, ok := f.(billy.Locker); ok {
			err = locker.Lock()
			if err != nil {
				return nil, err
			}
		}

		fi, err = d.fs.Stat(packedRefsPath)
		if err != nil {
			return nil, err
		}
		if mtime.Equal(fi.ModTime()) {
			break
		}
		// The file has changed since we opened it.  Close and retry.
		err = f.Close()
		if err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (d *DotGit) rewritePackedRefsWithoutRef(name plumbing.ReferenceName) (err error) {
	pr, err := d.openAndLockPackedRefs(false)
	if err != nil {
		return err
	}
	if pr == nil {
		return nil
	}
	defer ioutil.CheckClose(pr, &err)

	// Creating the temp file in the same directory as the target file
	// improves our chances for rename operation to be atomic.
	tmp, err := d.fs.TempFile("", tmpPackedRefsPrefix)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		ioutil.CheckClose(tmp, &err)
		_ = d.fs.Remove(tmpName) // don't check err, we might have renamed it
	}()

	s := bufio.NewScanner(pr)
	found := false
	for s.Scan() {
		line := s.Text()
		ref, err := d.processLine(line)
		if err != nil {
			return err
		}

		if ref != nil && ref.Name() == name {
			found = true
			continue
		}

		if _, err := fmt.Fprintln(tmp, line); err != nil {
			return err
		}
	}

	if err := s.Err(); err != nil {
		return err
	}

	if !found {
		return nil
	}

	return d.rewritePackedRefsWhileLocked(tmp, pr)
}

// process lines from a packed-refs file
func (d *DotGit) processLine(line string) (*plumbing.Reference, error) {
	if len(line) == 0 {
		return nil, nil
	}

	switch line[0] {
	case '#': // comment - ignore
		return nil, nil
	case '^': // annotated tag commit of the previous line - ignore
		return nil, nil
	default:
		ws := strings.Split(line, " ") // hash then ref
		if len(ws) != 2 {
			return nil, ErrPackedRefsBadFormat
		}

		return plumbing.NewReferenceFromStrings(ws[1], ws[0]), nil
	}
}

func (d *DotGit) addRefsFromRefDir(refs *[]*plumbing.Reference, seen map[plumbing.ReferenceName]bool) error {
	return d.walkReferencesTree(refs, []string{refsPath}, seen)
}

func (d *DotGit) walkReferencesTree(refs *[]*plumbing.Reference, relPath []string, seen map[plumbing.ReferenceName]bool) error {
	files, err := d.fs.ReadDir(d.fs.Join(relPath...))
	if err != nil {
		if os.IsNotExist(err) {
			// a race happened, and our directory is gone now
			return nil
		}

		return err
	}

	for _, f := range files {
		newRelPath := append(append([]string(nil), relPath...), f.Name())
		if f.IsDir() {
			if err = d.walkReferencesTree(refs, newRelPath, seen); err != nil {
				return err
			}

			continue
		}

		ref, err := d.readReferenceFile(".", strings.Join(newRelPath, "/"))
		if os.IsNotExist(err) {
			// a race happened, and our file is gone now
			continue
		}
		if err != nil {
			return err
		}

		if ref != nil && !seen[ref.Name()] {
			*refs = append(*refs, ref)
			seen[ref.Name()] = true
		}
	}

	return nil
}

func (d *DotGit) addRefFromHEAD(refs *[]*plumbing.Reference) error {
	ref, err := d.readReferenceFile(".", "HEAD")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	*refs = append(*refs, ref)
	return nil
}

func (d *DotGit) readReferenceFile(path, name string) (ref *plumbing.Reference, err error) {
	path = d.fs.Join(path, d.fs.Join(strings.Split(name, "/")...))
	st, err := d.fs.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, ErrIsDir
	}

	f, err := d.fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer ioutil.CheckClose(f, &err)

	return d.readReferenceFrom(f, name)
}

// CountLooseRefs returns the number of loose references in the repository.
func (d *DotGit) CountLooseRefs() (int, error) {
	var refs []*plumbing.Reference
	seen := make(map[plumbing.ReferenceName]bool)
	if err := d.addRefsFromRefDir(&refs, seen); err != nil {
		return 0, err
	}

	return len(refs), nil
}

// PackRefs packs all loose refs into the packed-refs file.
//
// This implementation only works under the assumption that the view
// of the file system won't be updated during this operation.  This
// strategy would not work on a general file system though, without
// locking each loose reference and checking it again before deleting
// the file, because otherwise an updated reference could sneak in and
// then be deleted by the packed-refs process.  Alternatively, every
// ref update could also lock packed-refs, so only one lock is
// required during ref-packing.  But that would worsen performance in
// the common case.
//
// TODO: add an "all" boolean like the `git pack-refs --all` flag.
// When `all` is false, it would only pack refs that have already been
// packed, plus all tags.
func (d *DotGit) PackRefs() (err error) {
	// Lock packed-refs, and create it if it doesn't exist yet.
	f, err := d.openAndLockPackedRefs(true)
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(f, &err)

	// Gather all refs using addRefsFromRefDir and addRefsFromPackedRefs.
	var refs []*plumbing.Reference
	seen := make(map[plumbing.ReferenceName]bool)
	if err = d.addRefsFromRefDir(&refs, seen); err != nil {
		return err
	}
	if len(refs) == 0 {
		// Nothing to do!
		return nil
	}
	numLooseRefs := len(refs)
	if err = d.addRefsFromPackedRefsFile(&refs, f, seen); err != nil {
		return err
	}

	// Write them all to a new temp packed-refs file.
	tmp, err := d.fs.TempFile("", tmpPackedRefsPrefix)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		ioutil.CheckClose(tmp, &err)
		_ = d.fs.Remove(tmpName) // don't check err, we might have renamed it
	}()

	w := bufio.NewWriter(tmp)
	for _, ref := range refs {
		_, err = w.WriteString(ref.String() + "\n")
		if err != nil {
			return err
		}
	}
	err = w.Flush()
	if err != nil {
		return err
	}

	// Rename the temp packed-refs file.
	err = d.rewritePackedRefsWhileLocked(tmp, f)
	if err != nil {
		return err
	}

	// Delete all the loose refs, while still holding the packed-refs
	// lock.
	for _, ref := range refs[:numLooseRefs] {
		path := d.fs.Join(".", ref.Name().String())
		err = d.fs.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// Module return a billy.Filesystem pointing to the module folder
//
// As a defence in depth against submodule name path traversal,
// refuse names whose joined path leaves the modules/ subtree once
// cleaned. The config-layer parser also validates submodule names,
// but Module may be reached from any caller that constructs a
// Submodule struct programmatically and so bypasses the parser.
func (d *DotGit) Module(name string) (billy.Filesystem, error) {
	p := d.fs.Join(modulePath, name)
	cleaned := path.Clean(filepath.ToSlash(p))
	if cleaned != modulePath && !strings.HasPrefix(cleaned, modulePath+"/") {
		return nil, ErrModuleNameEscape
	}
	return d.fs.Chroot(p)
}

// AddAlternate appends an alternate object directory path to the alternates file.
func (d *DotGit) AddAlternate(remote string) error {
	altpath := d.fs.Join(objectsPath, infoPath, alternatesPath)

	f, err := d.fs.OpenFile(altpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if locker, ok := f.(billy.Locker); ok {
		// locking in windows throws an error, based on comments
		// https://github.com/go-git/go-git/pull/860#issuecomment-1751823044
		// do not lock on windows platform.
		if runtime.GOOS != "windows" {
			if err = locker.Lock(); err != nil {
				return fmt.Errorf("cannot lock file: %w", err)
			}
			defer func() { _ = locker.Unlock() }()
		}
	}

	line := path.Join(remote, objectsPath) + "\n"
	_, err = io.WriteString(f, line)
	if err != nil {
		return fmt.Errorf("error writing 'alternates' file: %w", err)
	}

	return nil
}

// Alternates returns DotGit(s) based off paths in objects/info/alternates if
// available. This can be used to checks if it's a shared repository.
func (d *DotGit) Alternates() ([]*DotGit, error) {
	altpath := d.fs.Join(objectsPath, infoPath, alternatesPath)
	f, err := d.fs.Open(altpath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	fs := d.options.AlternatesFS
	if fs == nil {
		fs = d.fs
	}

	var alternates []*DotGit
	seen := make(map[string]struct{})

	// Read alternate paths line-by-line and create DotGit objects.
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		path := scanner.Text()
		if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
			path = filepath.Clean(path)
		} else {
			// By Git conventions, relative paths should be based on the object database (.git/objects/info)
			// location as per: https://www.kernel.org/pub/software/scm/git/docs/gitrepository-layout.html
			// However, due to the nature of go-git and its filesystem handling via Billy, paths cannot
			// cross its "chroot boundaries". Therefore, ignore any "../" and treat the path from the
			// fs root. If this is not correct based on the dotgit fs, set a different one via AlternatesFS.
			abs := filepath.Join(string(filepath.Separator), filepath.ToSlash(path))
			path = filepath.FromSlash(abs)
		}

		path = alternatePathForFS(path, fs.Root())

		// Avoid creating multiple dotgits for the same alternative path.
		if _, ok := seen[path]; ok {
			continue
		}

		seen[path] = struct{}{}

		// Aligns with upstream behavior: exit if target path is not a valid directory.
		if fi, err := fs.Stat(path); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("invalid object directory %q: %w", path, err)
		}
		afs, err := fs.Chroot(filepath.Dir(path))
		if err != nil {
			return nil, fmt.Errorf("cannot chroot %q: %w", path, err)
		}
		// Inherit the parent DotGit's ObjectFormat so the alternate
		// reads and writes hashes at the same width as the
		// repository it serves. Inherit the FD pool too so the
		// storage-wide budget covers alternate packs and idxes.
		// Other Options keep their defaults from New.
		alternates = append(alternates, NewWithOptions(afs, Options{
			ObjectFormat:      d.options.ObjectFormat,
			ReadReverseIndex:  true,
			WriteReverseIndex: true,
			Pool:              d.options.Pool,
		}))
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}

	return alternates, nil
}

func alternatePathForFS(path, root string) string {
	if root == "" {
		return path
	}

	root = filepath.Clean(root)
	if root == "." || root == string(filepath.Separator) {
		return path
	}

	rel, err := filepath.Rel(root, path)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}

	vol := filepath.VolumeName(path)
	if vol != "" {
		return strings.TrimLeft(path[len(vol):], `\/`)
	}

	return strings.TrimPrefix(path, root+string(filepath.Separator))
}

// Fs returns the underlying filesystem of the DotGit folder.
func (d *DotGit) Fs() billy.Filesystem {
	return d.fs
}

// SetObjectFormat configures the object format for this DotGit instance.
func (d *DotGit) SetObjectFormat(of formatcfg.ObjectFormat) error {
	d.options.ObjectFormat = of

	return nil
}

func isHex(s string) bool {
	for _, b := range []byte(s) {
		if isNum(b) {
			continue
		}
		if isHexAlpha(b) {
			continue
		}

		return false
	}

	return true
}

func isNum(b byte) bool {
	return b >= '0' && b <= '9'
}

func isHexAlpha(b byte) bool {
	return b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

// incBytes increments a byte slice, which involves incrementing the
// right-most byte, and following carry leftward.
// It makes a copy so that the provided slice's underlying array is not modified.
// If the overall operation overflows (e.g. incBytes(0xff, 0xff)), the second return parameter indicates that.
func incBytes(in []byte) (out []byte, overflow bool) {
	out = make([]byte, len(in))
	copy(out, in)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			return out, overflow // Didn't overflow.
		}
	}
	overflow = true
	return out, overflow
}
