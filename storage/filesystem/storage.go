// Package filesystem is a storage backend base on filesystems
package filesystem

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/helper/chroot"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/reflog"
	"github.com/go-git/go-git/v6/plumbing/format/reftable"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/x/fdpool"
)

// defaultPoolCapacity is the FD pool capacity selected when
// [Options.Pool] is nil. It accommodates roughly 85 concurrently-hot
// packs (3 FDs per pack: pack + idx + rev).
const defaultPoolCapacity = 256

// Storage is an implementation of git.Storer that stores data on disk in the
// standard git format (this is, the .git directory). Zero values of this type
// are not safe to use, see the NewStorage function below.
type Storage struct {
	fs     billy.Filesystem
	dir    *dotgit.DotGit
	hasher plumbing.Hasher

	ObjectStorage
	referenceStorage storer.ReferenceStorer
	IndexStorage
	ShallowStorage
	ConfigStorage
	ModuleStorage
	reflogStorage storer.ReflogStorer

	reftableStack *reftable.Stack // non-nil when using reftable backend
}

// Compile-time assertions pin both *Storage and *dotgit.DotGit to
// [storer.IdleReleaser]. *Storage promotes CloseIdleDescriptors
// from the embedded [ObjectStorage] via Go's method-set promotion
// rules; *dotgit.DotGit defines the method directly. A future
// rename or signature change on either side breaks the build
// immediately.
var (
	_ storer.IdleReleaser = (*Storage)(nil)
	_ storer.IdleReleaser = (*dotgit.DotGit)(nil)
)

// Options holds configuration for the storage.
type Options struct {
	// ExclusiveAccess means that the filesystem is not modified externally
	// while the repo is open.
	ExclusiveAccess bool
	// LargeObjectThreshold maximum object size (in bytes) that will be read in to memory.
	// If left unset or set to 0 there is no limit
	LargeObjectThreshold int64
	// AlternatesFS provides the billy filesystem to be used for Git Alternates.
	// If none is provided, it falls back to using the underlying instance used for
	// DotGit.
	AlternatesFS billy.Filesystem
	// HighMemoryMode defines whether the storage will operate in high-memory
	// mode. This defaults to false. For more information refer to packfile's Parser
	// WithHighMemoryMode option.
	HighMemoryMode bool

	// ObjectFormat defines the ObjectFormat when creating a new storage.
	// This value is completely ignored when the storage is pointing to an
	// existing dotgit. In such cases the repository config will define the
	// ObjectFormat - even if implicitly (e.g. SHA1).
	ObjectFormat formatcfg.ObjectFormat

	// UseInMemoryIdx loads .idx files fully into memory (MemoryIndex) instead
	// of reading them on demand via ReadAt (LazyIndex). This uses more memory
	// but avoids keeping file descriptors open. Defaults to false.
	UseInMemoryIdx bool

	// IndexCache provides an optional cache implementation for index data.
	// If left as nil, a default stat-based implementation is created automatically.
	IndexCache IndexCache

	// Pool governs LRU eviction of the read-side .pack/.idx/.rev
	// file descriptors that this Storage opens. When a Storage
	// exceeds the pool capacity the least-recently-used SharedFile
	// is closed (via ReleaseNow) and reopens on the next read.
	// Pack-write FDs ([PackWriter]) are short-lived and are not
	// pooled.
	//
	// A nil Pool selects a per-Storage pool sized at the package
	// default (currently 256 entries, accommodating roughly 85
	// concurrently-hot packs at 3 FDs per pack). The default
	// assumes go-git owns the dominant share of FDs in the
	// process; applications running other FD-heavy subsystems
	// (network servers, large connection pools) should construct
	// a pool sized against their full FD budget and pass it here
	// rather than rely on the default.
	//
	// Sharing one Pool across multiple Storages bounds the FD
	// budget process-wide rather than per Storage — useful for
	// servers handling concurrent per-request repositories or
	// batch tools iterating many repos. Construct the pool with
	// [fdpool.New], pass it via this field to
	// [NewStorageWithOptions], then open repositories with
	// [git.Open], [git.Clone], or [git.Init] using the resulting
	// Storer. The path-based wrappers (all Plain* functions:
	// PlainOpen, PlainClone, PlainInit) construct their own
	// Storage internally and so do not accept an injected pool;
	// that is by design.
	//
	// To disable pooling, pass [fdpool.New](0) (or any non-positive
	// capacity): the resulting no-op Pool causes pool-less
	// SharedFiles to fall back to their grace-period close on
	// quiescence.
	//
	// To request mmap-backed read FDs (read-only, where the
	// platform supports it) construct the underlying billy
	// filesystem with [github.com/go-git/go-billy/v6/osfs.WithMmap]
	// before handing it to [NewStorageWithOptions]. The pool
	// governs that file equally whether it is FD- or mmap-backed.
	//
	// The Pool field's API stability tracks [fdpool.Pool]'s, not
	// this package's. Per the x/ package policy, the fdpool API
	// may change without following semantic versioning; consumers
	// reading this field should treat it as experimental on the
	// same timeline.
	Pool *fdpool.Pool
}

// NewStorage returns a new Storage backed by a given `fs.Filesystem` and cache.
func NewStorage(fs billy.Filesystem, cache cache.Object) *Storage {
	return NewStorageWithOptions(fs, cache, Options{})
}

// NewStorageWithOptions returns a new Storage with extra options,
// backed by a given `fs.Filesystem` and cache.
// Returns an error if an explicit ObjectFormat is provided via options
// but conflicts with an existing config in the filesystem.
func NewStorageWithOptions(fs billy.Filesystem, c cache.Object, ops Options) *Storage {
	// Reverse index defaults (true); overridden by repo config below.
	readRevIdx := true
	writeRevIdx := true
	skipHash := false
	var refStorage formatcfg.RefStorage

	f, err := fs.Open("config")
	if err == nil {
		cfg, err := config.ReadConfig(f)
		if err == nil {
			ops.ObjectFormat = cfg.Extensions.ObjectFormat
			readRevIdx = cfg.Pack.ReadReverseIndex
			writeRevIdx = cfg.Pack.WriteReverseIndex
			skipHash = cfg.Index.SkipHash.IsTrue()
			refStorage = cfg.Extensions.RefStorage
		}

		_ = f.Close()
	}

	hasher := plumbing.NewHasher(ops.ObjectFormat, plumbing.AnyObject, 0)

	// Construct a per-Storage FD pool at the package default
	// capacity unless the caller injected one. Pass a non-positive-
	// capacity Pool via Options.Pool to disable pooling.
	pool := ops.Pool
	if pool == nil {
		pool = fdpool.New(defaultPoolCapacity)
	}

	dirOps := dotgit.Options{
		ExclusiveAccess:   ops.ExclusiveAccess,
		AlternatesFS:      ops.AlternatesFS,
		ObjectFormat:      ops.ObjectFormat,
		ReadReverseIndex:  readRevIdx,
		WriteReverseIndex: writeRevIdx,
		Pool:              pool,
	}
	dir := dotgit.NewWithOptions(fs, dirOps)

	if c == nil {
		c = cache.NewObjectLRUDefault()
	}

	if ops.IndexCache == nil {
		ops.IndexCache = NewIndexCache()
	}

	s := &Storage{
		fs:     fs,
		dir:    dir,
		hasher: hasher,

		ObjectStorage:  *NewObjectStorageWithOptions(dir, c, ops),
		IndexStorage:   IndexStorage{dir: dir, h: hasher.Hash, cache: ops.IndexCache, skipHash: skipHash},
		ShallowStorage: ShallowStorage{dir: dir},
		ConfigStorage:  ConfigStorage{dir: dir, objectFormat: ops.ObjectFormat},
		ModuleStorage:  ModuleStorage{dir: dir, objectFormat: ops.ObjectFormat},
	}

	if refStorage == formatcfg.RefStorageReftable {
		reftableFS, err := chrootIfExists(fs, "reftable")
		switch {
		case err != nil:
			s.referenceStorage = &brokenReferenceStorage{err: err}
			s.reflogStorage = &brokenReflogStorage{err: err}
		case reftableFS != nil:
			stack, err := reftable.OpenStack(reftableFS)
			if err != nil {
				s.referenceStorage = &brokenReferenceStorage{err: err}
				s.reflogStorage = &brokenReflogStorage{err: err}
			} else {
				s.reftableStack = stack
				s.referenceStorage = &ReftableReferenceStorage{stack: stack}
				s.reflogStorage = &ReftableReflogStorage{stack: stack}
			}
		default:
			// reftableFS is nil. If the repo is configured for reftables, we must create
			// the directory and open the stack rather than falling back to loose refs.
			err := fs.MkdirAll("reftable", 0o755)
			if err != nil {
				s.referenceStorage = &brokenReferenceStorage{err: err}
				s.reflogStorage = &brokenReflogStorage{err: err}
			} else {
				reftableFS = chroot.New(fs, "reftable")
				stack, err := reftable.OpenStack(reftableFS)
				if err != nil {
					s.referenceStorage = &brokenReferenceStorage{err: err}
					s.reflogStorage = &brokenReflogStorage{err: err}
				} else {
					s.reftableStack = stack
					s.referenceStorage = &ReftableReferenceStorage{stack: stack}
					s.reflogStorage = &ReftableReflogStorage{stack: stack}
				}
			}
		}
	}

	if s.referenceStorage == nil {
		s.referenceStorage = &ReferenceStorage{dir: dir}
	}
	if s.reflogStorage == nil {
		s.reflogStorage = &ReflogStorage{dir: dir}
	}

	return s
}

// chrootIfExists returns a chrooted filesystem if the directory exists.
func chrootIfExists(fs billy.Filesystem, path string) (billy.Filesystem, error) {
	_, err := fs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return chroot.New(fs, path), nil
}

// SetObjectFormat sets the ObjectFormat for the storage, initiatising
// hashers and object hashers accordingly. This must only be called
// during the first pack negotiation of a repository clone operation.
//
// If the storage is empty and the new ObjectFormat is the same as the
// current, this call will be treated as a no-op.
func (s *Storage) SetObjectFormat(of formatcfg.ObjectFormat) error {
	switch of {
	case formatcfg.SHA1, formatcfg.SHA256:
	default:
		return fmt.Errorf("invalid object format: %s", of)
	}

	// Presently, storage only supports a single object format at a
	// time. Changing the format of an existing (and populated) object
	// storage is yet to be supported.
	packs, _ := s.dir.ObjectPacks()
	if len(packs) > 0 {
		return errors.New("cannot change object format of existing object storage")
	}

	cfg, err := s.Config()
	if err != nil {
		return err
	}

	if cfg.Extensions.ObjectFormat != of {
		cfg.Extensions.ObjectFormat = of
		cfg.Core.RepositoryFormatVersion = formatcfg.Version1
		err = s.SetConfig(cfg)
		if err != nil {
			return fmt.Errorf("cannot set object format on config: %w", err)
		}

		err = s.dir.SetObjectFormat(of)
		if err != nil {
			return fmt.Errorf("cannot set object format on dotgit: %w", err)
		}

		s.ConfigStorage.objectFormat = of
		s.ModuleStorage.objectFormat = of
		s.options.ObjectFormat = of
		s.oh = plumbing.FromObjectFormat(of)
		s.hasher = plumbing.NewHasher(of, plumbing.AnyObject, 0)
		s.h = s.hasher.Hash
	}

	return nil
}

// SupportsExtension checks whether the Storer supports the given
// Git extension defined by name.
func (s *Storage) SupportsExtension(name, value string) bool {
	switch name {
	case "objectformat":
		switch value {
		case "sha1", "sha256", "":
			return true
		}
	case "worktreeconfig":
		switch value {
		case "true", "false":
			return true
		}
	case "refstorage":
		if value == "reftable" {
			return true
		}
	}
	return false
}

// Filesystem returns the underlying filesystem
func (s *Storage) Filesystem() billy.Filesystem {
	return s.fs
}

// Init initializes .git directory
func (s *Storage) Init() error {
	return s.dir.Initialize()
}

// AddAlternate adds an alternate object directory and resets the cached
// alternate state so that subsequent object lookups pick up the new alternate.
func (s *Storage) AddAlternate(remote string) error {
	if err := s.dir.AddAlternate(remote); err != nil {
		return err
	}
	s.resetAlternates()
	return nil
}

// LowMemoryMode returns true if low memory mode is enabled.
func (s *Storage) LowMemoryMode() bool {
	return !s.options.HighMemoryMode
}

// SetReference stores a reference.
func (s *Storage) SetReference(ref *plumbing.Reference) error {
	return s.referenceStorage.SetReference(ref)
}

// CheckAndSetReference stores a reference after verifying the old value matches.
func (s *Storage) CheckAndSetReference(newRef, old *plumbing.Reference) error {
	return s.referenceStorage.CheckAndSetReference(newRef, old)
}

// Reference returns the reference with the given name.
func (s *Storage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	return s.referenceStorage.Reference(n)
}

// IterReferences returns an iterator over all references.
func (s *Storage) IterReferences() (storer.ReferenceIter, error) {
	return s.referenceStorage.IterReferences()
}

// RemoveReference deletes the reference with the given name.
func (s *Storage) RemoveReference(n plumbing.ReferenceName) error {
	return s.referenceStorage.RemoveReference(n)
}

// CountLooseRefs returns the number of loose references.
func (s *Storage) CountLooseRefs() (int, error) {
	return s.referenceStorage.CountLooseRefs()
}

// PackRefs packs all loose references.
func (s *Storage) PackRefs() error {
	return s.referenceStorage.PackRefs()
}

// Reflog returns the reflog entries for the given reference.
func (s *Storage) Reflog(name plumbing.ReferenceName) ([]*reflog.Entry, error) {
	return s.reflogStorage.Reflog(name)
}

// AppendReflog appends a single entry to the reflog for the given reference.
func (s *Storage) AppendReflog(name plumbing.ReferenceName, entry *reflog.Entry) error {
	return s.reflogStorage.AppendReflog(name, entry)
}

// DeleteReflog removes the entire reflog for the given reference.
func (s *Storage) DeleteReflog(name plumbing.ReferenceName) error {
	return s.reflogStorage.DeleteReflog(name)
}

type brokenReferenceStorage struct {
	err error
}

func (b *brokenReferenceStorage) SetReference(_ *plumbing.Reference) error {
	return b.err
}

func (b *brokenReferenceStorage) CheckAndSetReference(_, _ *plumbing.Reference) error {
	return b.err
}

func (b *brokenReferenceStorage) Reference(_ plumbing.ReferenceName) (*plumbing.Reference, error) {
	return nil, b.err
}

func (b *brokenReferenceStorage) IterReferences() (storer.ReferenceIter, error) {
	return nil, b.err
}

func (b *brokenReferenceStorage) RemoveReference(_ plumbing.ReferenceName) error {
	return b.err
}

func (b *brokenReferenceStorage) CountLooseRefs() (int, error) {
	return 0, b.err
}

func (b *brokenReferenceStorage) PackRefs() error {
	return b.err
}

type brokenReflogStorage struct {
	err error
}

func (b *brokenReflogStorage) Reflog(_ plumbing.ReferenceName) ([]*reflog.Entry, error) {
	return nil, b.err
}

func (b *brokenReflogStorage) AppendReflog(_ plumbing.ReferenceName, _ *reflog.Entry) error {
	return b.err
}

func (b *brokenReflogStorage) DeleteReflog(_ plumbing.ReferenceName) error {
	return b.err
}
