// Package filesystem is a storage backend base on filesystems
package filesystem

import (
	"errors"
	"fmt"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/x/fdpool"
)

// defaultMaxOpenDescriptors is the FD pool capacity selected when
// [Options.MaxOpenDescriptors] is zero. It accommodates roughly 85
// concurrently-hot packs (3 FDs per pack: pack + idx + rev).
const defaultMaxOpenDescriptors = 256

// Storage is an implementation of git.Storer that stores data on disk in the
// standard git format (this is, the .git directory). Zero values of this type
// are not safe to use, see the NewStorage function below.
type Storage struct {
	fs     billy.Filesystem
	dir    *dotgit.DotGit
	hasher plumbing.Hasher

	ObjectStorage
	ReferenceStorage
	IndexStorage
	ShallowStorage
	ConfigStorage
	ModuleStorage
	ReflogStorage
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
	// MaxOpenDescriptors is the capacity of the storage-wide LRU FD
	// pool. The pool bounds the read-side .pack/.idx/.rev file
	// descriptors across the entire Storage. When a Storage exceeds
	// this capacity, the least-recently-used `sharedFile`'s FD is
	// closed (via `sharedFile.ReleaseNow`) and reopens on the next
	// read. Pack-write FDs ([PackWriter]) are short-lived and not
	// pooled.
	//
	// Zero (the default) selects [defaultMaxOpenDescriptors] (256),
	// which accommodates roughly 85 concurrently-hot packs (3 FDs
	// per pack: pack + idx + rev). The 256 default assumes go-git
	// is responsible for the dominant share of FDs in the process;
	// applications running other FD-heavy subsystems (network
	// servers, large connection pools) should size this against
	// their full FD budget rather than rely on the default.
	// Negative values disable pooling: pool-less sharedFiles fall
	// back to their grace-period close on quiescence.
	//
	// The field name is reused from a pre-v6 option that capped
	// concurrently-open packs without an eviction policy; the v6
	// pool governs the same FD-budget concern with LRU eviction
	// across all .pack/.idx/.rev descriptors. The v5
	// KeepDescriptors flag (which pinned every pack FD open) is
	// removed; callers migrating from it can leave this field at
	// zero for the LRU default.
	//
	// To request mmap-backed read FDs (read-only, where the
	// platform supports it) construct the underlying billy
	// filesystem with [github.com/go-git/go-billy/v6/osfs.WithMmap]
	// before handing it to [NewStorageWithOptions]. The pool
	// governs that file equally whether it is FD- or mmap-backed.
	MaxOpenDescriptors int
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

	// Pool, when non-nil, replaces the per-Storage FD pool that
	// NewStorageWithOptions would otherwise construct. Multiple
	// Storages sharing a pool share a single bounded FD budget
	// across the process — useful when a single process opens
	// many Storages and wants the FD budget bounded process-wide
	// rather than per Storage (servers handling concurrent
	// requests, batch tools iterating many repositories, etc.).
	// When non-nil, MaxOpenDescriptors is ignored (the shared
	// pool's existing capacity governs).
	//
	// To share a pool across Storage instances, construct the
	// pool explicitly and pass it via this field to
	// [NewStorageWithOptions], then open repositories with
	// [git.Open], [git.Clone], or [git.Init] using the resulting
	// Storer. The path-based wrappers (all Plain* functions:
	// PlainOpen, PlainClone, PlainInit) construct their own
	// Storage internally and so do not accept an injected pool;
	// that is by design.
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

	f, err := fs.Open("config")
	if err == nil {
		cfg, err := config.ReadConfig(f)
		if err == nil {
			ops.ObjectFormat = cfg.Extensions.ObjectFormat
			readRevIdx = cfg.Pack.ReadReverseIndex
			writeRevIdx = cfg.Pack.WriteReverseIndex
			skipHash = cfg.Index.SkipHash.IsTrue()
		}

		_ = f.Close()
	}

	hasher := plumbing.NewHasher(ops.ObjectFormat, plumbing.AnyObject, 0)

	// Construct the FD pool unless the caller injected one. Zero
	// MaxOpenDescriptors selects the default capacity; negative
	// capacity yields a no-op pool whose Touch and Forget are
	// no-ops, so pool-less sharedFiles fall back to their grace
	// timer on quiescence.
	pool := ops.Pool
	if pool == nil {
		poolCap := ops.MaxOpenDescriptors
		if poolCap == 0 {
			poolCap = defaultMaxOpenDescriptors
		}
		pool = fdpool.New(poolCap)
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

		ObjectStorage:    *NewObjectStorageWithOptions(dir, c, ops),
		ReferenceStorage: ReferenceStorage{dir: dir},
		IndexStorage:     IndexStorage{dir: dir, h: hasher.Hash, cache: ops.IndexCache, skipHash: skipHash},
		ShallowStorage:   ShallowStorage{dir: dir},
		ConfigStorage:    ConfigStorage{dir: dir, objectFormat: ops.ObjectFormat},
		ModuleStorage:    ModuleStorage{dir: dir, objectFormat: ops.ObjectFormat},
		ReflogStorage:    ReflogStorage{dir: dir},
	}

	return s
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
