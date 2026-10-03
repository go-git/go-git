// Package filesystem provides a merkletrie noder implementation for billy filesystems.
package filesystem

import (
	"io"
	iofs "io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/gitignore"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/utils/convert"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
	"github.com/go-git/go-git/v6/utils/sync"
)

var ignore = map[string]bool{
	".git": true,
}

// Options contains configuration for the filesystem node.
type Options struct {
	// AutoCRLF converts CRLF line endings in text files into LF line endings.
	AutoCRLF bool

	// Index is used to enable the metadata-first comparison optimization while
	// correctly handling the "racy git" condition. If no index is provided,
	// the function works without the optimization.
	Index *index.Index

	// IgnoreScope, if non-nil, is consulted while walking the tree. Untracked
	// entries (files or directories) that it reports as ignored are excluded
	// from the walk, so callers do not have to descend into large gitignored
	// directories like node_modules. Tracked entries are always walked even
	// when ignored, so modifications to them are still reported.
	//
	// It is the scope in effect at the root of the walk, normally
	// gitignore.NewScope of gitignore.RootPatterns plus any patterns the
	// caller supplies. The walk derives each directory's scope from the
	// listing it already takes, so a .gitignore is opened only in directories
	// actually visited and never below an excluded one, and an excluded
	// directory stays authoritative for everything under it.
	//
	// Requires Index to be set: without an index there is no way to identify
	// tracked entries, so the scope is treated as a no-op.
	IgnoreScope *gitignore.Scope

	// CollapseUntrackedDirs, when true, makes directories that contain no
	// tracked entries and at least one visible (non-ignored) file report
	// as a single change for the directory itself instead of being walked,
	// matching the default behavior of "git status". Directories without
	// any visible content produce no change at all, matching git not
	// listing empty untracked directories. Requires Index to be set:
	// without an index there is no way to identify tracked entries, so
	// the option is treated as a no-op.
	CollapseUntrackedDirs bool
}

// The node represents a file or a directory in a billy.Filesystem. It
// implements the interface noder.Noder of merkletrie package.
//
// This implementation implements a "standard" hash method being able to be
// compared with any other noder.Noder implementation inside of go-git.
type node struct {
	fs         billy.Filesystem
	submodules map[string]plumbing.Hash
	idx        *index.Index
	idxMap     map[string]*index.Entry
	// trackedDirs holds every directory path that has at least one entry
	// in the index. It is populated only when IgnoreScope or
	// CollapseUntrackedDirs is set so the walker can keep tracked entries
	// even if their parent directory matches an ignore rule.
	trackedDirs map[string]struct{}

	options *Options

	// scope is the ignore scope governing this node's entries. On a child it
	// starts as the parent's scope and is replaced by this directory's own on
	// the first calculateChildren, which is when the listing that reveals
	// whether a .gitignore is present becomes available. scopeResolved tracks
	// that transition; the root node is created already resolved.
	scope         *gitignore.Scope
	scopeResolved bool

	path     string
	hash     []byte
	children []noder.Noder
	isDir    bool
	mode     os.FileMode
	size     int64
	modTime  time.Time
}

// NewRootNode returns the root node based on a given billy.Filesystem.
//
// In order to provide the submodule hash status, a map[string]plumbing.Hash
// should be provided where the key is the path of the submodule and the commit
// of the submodule HEAD
//
// Deprecated: Use NewRootNodeWithOptions instead for better performance.
// This function is kept for backward compatibility.
func NewRootNode(
	fs billy.Filesystem,
	submodules map[string]plumbing.Hash,
) noder.Noder {
	return NewRootNodeWithOptions(fs, submodules, Options{Index: nil})
}

// NewRootNodeWithOptions returns the root node based on a given billy.Filesystem
// with options for CRLF handling and an index. Providing an index enables the
// metadata-first comparison optimization while correctly handling the "racy git"
// condition. If no index is provided, the function works without the optimization.
//
// The index's ModTime field is used to detect the racy git condition. When a file's
// mtime equals or is newer than the index ModTime, we must hash the file content
// even if other metadata matches, because the file may have been modified in the
// same second that the index was written.
//
// Reference: https://git-scm.com/docs/racy-git
func NewRootNodeWithOptions(
	fs billy.Filesystem,
	submodules map[string]plumbing.Hash,
	options Options,
) noder.Noder {
	var idxMap map[string]*index.Entry
	var trackedDirs map[string]struct{}

	if options.Index != nil {
		idxMap = make(map[string]*index.Entry, len(options.Index.Entries))
		for _, entry := range options.Index.Entries {
			idxMap[entry.Name] = entry
		}

		if options.IgnoreScope != nil || options.CollapseUntrackedDirs {
			trackedDirs = make(map[string]struct{})
			for _, entry := range options.Index.Entries {
				for parent := path.Dir(entry.Name); parent != "." && parent != "/"; parent = path.Dir(parent) {
					if _, ok := trackedDirs[parent]; ok {
						break
					}
					trackedDirs[parent] = struct{}{}
				}
			}
		}
	}

	return &node{
		fs:          fs,
		submodules:  submodules,
		idx:         options.Index,
		idxMap:      idxMap,
		trackedDirs: trackedDirs,
		options:     &options,
		isDir:       true,
		// The root scope already accounts for the root's own ignore files, so
		// it must not descend again.
		scope:         options.IgnoreScope,
		scopeResolved: true,
	}
}

// Hash the hash of a filesystem is the result of concatenating the computed
// plumbing.Hash of the file as a Blob and its plumbing.FileMode; that way the
// difftree algorithm will detect changes in the contents of files and also in
// their mode.
//
// Please note that the hash is calculated on first invocation of Hash(),
// meaning that it will not update when the underlying file changes
// between invocations.
//
// The hash of a directory is always a 24-bytes slice of zero values
func (n *node) Hash() []byte {
	if n.hash == nil {
		n.calculateHash()
	}
	return n.hash
}

func (n *node) Name() string {
	return path.Base(n.path)
}

func (n *node) IsDir() bool {
	return n.isDir
}

func (n *node) Skip() bool {
	return false
}

func (n *node) Children() ([]noder.Noder, error) {
	if err := n.calculateChildren(); err != nil {
		return nil, err
	}

	return n.children, nil
}

func (n *node) NumChildren() (int, error) {
	if err := n.calculateChildren(); err != nil {
		return -1, err
	}

	return len(n.children), nil
}

func (n *node) calculateChildren() error {
	if !n.IsDir() {
		return nil
	}

	if len(n.children) != 0 {
		return nil
	}

	files, err := n.fs.ReadDir(n.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := n.resolveScope(files); err != nil {
		return err
	}

	for _, file := range files {
		if _, ok := ignore[file.Name()]; ok {
			continue
		}

		fi, err := file.Info()
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSocket != 0 {
			continue
		}

		if n.shouldSkipIgnored(file.Name(), fi.IsDir()) {
			continue
		}

		c, err := n.newChildNode(fi)
		if err != nil {
			return err
		}

		n.children = append(n.children, c)
	}

	return nil
}

// resolveScope derives this directory's ignore scope from the listing just
// taken for it. Deferring to this point is the whole benefit of the scoped
// walk: whether a .gitignore exists is read off a listing the walk needed
// anyway, the file is opened only in directories actually visited, and
// Scope.Descend declines to open it at all below an excluded directory.
func (n *node) resolveScope(files []iofs.DirEntry) error {
	if n.scopeResolved {
		return nil
	}
	n.scopeResolved = true

	scope, err := descendScope(n.fs, n.scope, n.path, files)
	if err != nil {
		return err
	}
	if scope != nil {
		n.scope = scope
	}

	return nil
}

// descendScope derives the scope governing the entries of the directory at
// dirPath from its parent's scope and the listing just taken for it,
// reading the directory's own .gitignore only when the listing shows one.
// A nil parent scope means no ignore filtering is in effect and yields a
// nil scope. It mirrors resolveScope for directories the collapse scan
// visits without creating nodes.
func descendScope(fs billy.Filesystem, parent *gitignore.Scope, dirPath string, files []iofs.DirEntry) (*gitignore.Scope, error) {
	if parent == nil {
		return nil, nil
	}

	var readOwn func() ([]gitignore.Pattern, error)
	for _, f := range files {
		if f.Name() == gitignore.IgnoreFile && !f.IsDir() {
			dir := pathComponents(dirPath)
			readOwn = func() ([]gitignore.Pattern, error) {
				return gitignore.DirPatterns(fs, dir)
			}
			break
		}
	}

	return parent.Descend(pathComponents(dirPath), readOwn)
}

func pathComponents(p string) []string {
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// shouldSkipIgnored reports whether the child entry of n with the given
// name should be skipped because it matches the ignore scope in effect
// AND has no entry in the index. Tracked entries are never skipped so
// modifications to them are still reported.
func (n *node) shouldSkipIgnored(name string, isDir bool) bool {
	return n.isIgnoredUntracked(n.scope, path.Join(n.path, name), isDir)
}

// isIgnoredUntracked reports whether the entry at childPath should be
// skipped because scope matches it AND it has no entry in the index.
// scope must be the one governing the entries of childPath's parent
// directory; a nil scope means no ignore filtering is in effect.
func (n *node) isIgnoredUntracked(scope *gitignore.Scope, childPath string, isDir bool) bool {
	if scope == nil {
		return false
	}
	// Without an index we cannot prove that a subtree contains no tracked
	// entries, so refuse to skip. This matches the documented contract on
	// Options.IgnoreScope.
	if n.idxMap == nil {
		return false
	}
	if !scope.Match(strings.Split(childPath, "/"), isDir) {
		return false
	}
	// An entry whose own path is in the index is tracked, regardless of
	// whether it is a regular file or a directory-shaped entry such as a
	// submodule. Submodule entries' paths are *not* added to trackedDirs
	// (which only records parent chains), so this check has to come first.
	if _, tracked := n.idxMap[childPath]; tracked {
		return false
	}
	if isDir {
		_, hasTrackedDescendant := n.trackedDirs[childPath]
		return !hasTrackedDescendant
	}
	return true
}

// Collapse implements noder.Collapser. It reports true when the node is a
// directory that has no tracked descendants but contains at least one
// visible (non-ignored) file, so the whole subtree can be represented by
// a single change for the directory itself. Directories without any
// visible content are not collapsed: they must produce no change at all,
// matching git which does not list empty untracked directories.
func (n *node) Collapse() bool {
	if n.options == nil || !n.options.CollapseUntrackedDirs || !n.isDir {
		return false
	}
	if n.idxMap == nil {
		return false
	}
	if _, tracked := n.trackedDirs[n.path]; tracked {
		return false
	}
	return n.containsVisibleEntry()
}

// containsVisibleEntry reports whether the subtree rooted at n contains at
// least one entry that would surface in a walk: a non-ignored file,
// symlink included. The scan short-circuits on the first visible entry,
// so fully untracked directories are usually validated with a single
// ReadDir per nesting level instead of a full enumeration.
func (n *node) containsVisibleEntry() bool {
	// pending directories carry the scope inherited from their parent and
	// derive their own from the listing taken for them here, as walked
	// nodes do in resolveScope, so a .gitignore below n is honored.
	type pending struct {
		path  string
		scope *gitignore.Scope
	}

	dirs := []pending{{n.path, n.scope}}
	for len(dirs) > 0 {
		d := dirs[len(dirs)-1]
		dirs = dirs[:len(dirs)-1]

		files, err := n.fs.ReadDir(d.path)
		if err != nil {
			// Refuse to collapse so the full walk surfaces the error.
			return false
		}

		scope, err := descendScope(n.fs, d.scope, d.path, files)
		if err != nil {
			return false
		}

		for _, file := range files {
			if _, ok := ignore[file.Name()]; ok {
				continue
			}
			if file.Type()&os.ModeSocket != 0 {
				continue
			}
			isDir := file.IsDir()
			childPath := path.Join(d.path, file.Name())
			if n.isIgnoredUntracked(scope, childPath, isDir) {
				continue
			}
			if !isDir {
				return true
			}
			dirs = append(dirs, pending{childPath, scope})
		}
	}
	return false
}

func (n *node) newChildNode(file os.FileInfo) (*node, error) {
	path := path.Join(n.path, file.Name())

	node := &node{
		fs:          n.fs,
		submodules:  n.submodules,
		idx:         n.idx,
		idxMap:      n.idxMap,
		trackedDirs: n.trackedDirs,
		options:     n.options,

		// The child inherits this directory's scope and resolves its own on
		// its first listing.
		scope: n.scope,

		path:    path,
		isDir:   file.IsDir(),
		size:    file.Size(),
		mode:    file.Mode(),
		modTime: file.ModTime(),
	}

	if _, isSubmodule := n.submodules[path]; isSubmodule {
		node.isDir = false
	}

	return node, nil
}

func (n *node) calculateHash() {
	if n.isDir {
		n.hash = make([]byte, 24)
		return
	}
	mode, err := filemode.NewFromOSFileMode(n.mode)
	if err != nil {
		n.hash = plumbing.ZeroHash.Bytes()
		return
	}
	if submoduleHash, isSubmodule := n.submodules[n.path]; isSubmodule {
		n.hash = append(submoduleHash.Bytes(), filemode.Submodule.Bytes()...)
		return
	}

	if n.idxMap != nil {
		if entry, ok := n.idxMap[n.path]; ok {
			if n.metadataMatches(entry) {
				n.hash = append(entry.Hash.Bytes(), mode.Bytes()...)
				return
			}
		}
	}

	var hash plumbing.Hash
	if n.mode&os.ModeSymlink != 0 {
		hash = n.doCalculateHashForSymlink()
	} else {
		hash = n.doCalculateHashForRegular()
	}
	n.hash = append(hash.Bytes(), mode.Bytes()...)
}

func (n *node) metadataMatches(entry *index.Entry) bool {
	if entry == nil {
		return false
	}

	if uint32(n.size) != entry.Size {
		return false
	}

	if !n.modTime.IsZero() && !n.modTime.Equal(entry.ModifiedAt) {
		return false
	}

	mode, err := filemode.NewFromOSFileMode(n.mode)
	if err != nil {
		return false
	}

	if mode != entry.Mode {
		return false
	}

	if n.idx != nil && !n.idx.ModTime.IsZero() && !n.modTime.IsZero() {
		if !n.modTime.Before(n.idx.ModTime) {
			return false
		}
	}

	// If we couldn't perform the racy git check (idx is nil or idx.ModTime is zero),
	// we cannot safely rely on metadata alone — force content hashing.
	// This can occur with in-memory storage where the index file timestamp is unavailable.
	if n.idx == nil || n.idx.ModTime.IsZero() {
		return false
	}

	return true
}

func (n *node) doCalculateHashForRegular() plumbing.Hash {
	f, err := n.fs.Open(n.path)
	if err != nil {
		return plumbing.ZeroHash
	}
	defer func() { _ = f.Close() }()

	h := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, n.size)
	var dst io.Writer = h

	if n.options != nil && n.options.AutoCRLF {
		br := sync.GetBufioReader(f)
		defer sync.PutBufioReader(br)

		stat, err := convert.GetStat(br)
		if err != nil {
			return plumbing.ZeroHash
		}

		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return plumbing.ZeroHash
		}

		if !stat.IsBinary() {
			h.Reset(plumbing.BlobObject, n.size-int64(stat.CRLF))
			dst = convert.NewLFWriter(dst)
		}
	}

	if _, err := ioutil.CopyBufferPool(dst, f); err != nil {
		return plumbing.ZeroHash
	}

	return h.Sum()
}

func (n *node) doCalculateHashForSymlink() plumbing.Hash {
	target, err := n.fs.Readlink(n.path)
	if err != nil {
		return plumbing.ZeroHash
	}

	h := plumbing.NewHasher(format.SHA1, plumbing.BlobObject, n.size)
	if _, err := h.Write([]byte(target)); err != nil {
		return plumbing.ZeroHash
	}

	return h.Sum()
}

func (n *node) String() string {
	return n.path
}
