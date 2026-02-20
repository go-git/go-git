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
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

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
}

// Options holds configuration for the storage.
type Options struct {
	// ExclusiveAccess means that the filesystem is not modified externally
	// while the repo is open.
	ExclusiveAccess bool
	// KeepDescriptors makes the file descriptors to be reused but they will
	// need to be manually closed calling Close().
	KeepDescriptors bool
	// MaxOpenDescriptors is the max number of file descriptors to keep
	// open. If KeepDescriptors is true, all file descriptors will remain open.
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
	f, err := fs.Open("config")
	if err == nil {
		cfg, err := config.ReadConfig(f)
		if err == nil {
			ops.ObjectFormat = cfg.Extensions.ObjectFormat
		}

		_ = f.Close()
	}

	hasher := plumbing.NewHasher(ops.ObjectFormat, plumbing.AnyObject, 0)

	dirOps := dotgit.Options{
		ExclusiveAccess: ops.ExclusiveAccess,
		AlternatesFS:    ops.AlternatesFS,
		KeepDescriptors: ops.KeepDescriptors,
		ObjectFormat:    ops.ObjectFormat,
	}
	dir := dotgit.NewWithOptions(fs, dirOps)

	if c == nil {
		c = cache.NewObjectLRUDefault()
	}

	s := &Storage{
		fs:     fs,
		dir:    dir,
		hasher: hasher,

		ObjectStorage:    *NewObjectStorageWithOptions(dir, c, ops),
		ReferenceStorage: ReferenceStorage{dir: dir},
		IndexStorage:     IndexStorage{dir: dir, h: hasher.Hash},
		ShallowStorage:   ShallowStorage{dir: dir},
		ConfigStorage:    ConfigStorage{dir: dir, objectFormat: ops.ObjectFormat},
		ModuleStorage:    ModuleStorage{dir: dir},
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

		s.hasher = plumbing.NewHasher(of, plumbing.AnyObject, 0)
		s.h = s.hasher.Hash
	}

	return nil
}

// SupportsExtension checks whether the Storer supports the given
// Git extension defined by name.
func (s *Storage) SupportsExtension(name, value string) bool {
	if name != "objectformat" {
		return false
	}

	switch value {
	case "sha1", "sha256", "":
		return true
	default:
		return false
	}
}

// Filesystem returns the underlying filesystem
func (s *Storage) Filesystem() billy.Filesystem {
	return s.fs
}

// Init initializes .git directory
func (s *Storage) Init() error {
	return s.dir.Initialize()
}

// AddAlternate adds an alternate object directory.
func (s *Storage) AddAlternate(remote string) error {
	return s.dir.AddAlternate(remote)
}

// LowMemoryMode returns true if low memory mode is enabled.
func (s *Storage) LowMemoryMode() bool {
	return !s.options.HighMemoryMode
}
