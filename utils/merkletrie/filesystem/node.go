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

	// Prefix, if non-empty, scopes the walk to the subtree at that path: only
	// entries under it, and the directories on the way to it, are visited.
	// Entries of any other directory are left out as the directory is listed,
	// so their contents are never read or hashed. The tree the walk yields is
	// then the subtree of the full walk's tree, with the same paths and the
	// same decisions taken about each of them: the prefix's ancestors are
	// still listed and still apply their ignore scope and their skip rules to
	// the components of the path.
	//
	// It is the caller's responsibility to request a prefix whose entries are
	// wanted; the empty prefix is the whole tree, as before.
	Prefix string
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
	// in the index. It is populated only when IgnoreScope is set so the
	// walker can keep tracked entries even if their parent directory
	// matches an ignore rule.
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
	idxMap, trackedDirs := indexMaps(options.Index, options.IgnoreScope, options.Prefix)

	return &node{
		fs:            fs,
		submodules:    submodules,
		idx:           options.Index,
		idxMap:        idxMap,
		trackedDirs:   trackedDirs,
		options:       &options,
		isDir:         true,
		scope:         options.IgnoreScope,
		scopeResolved: true,
	}
}

// indexMaps returns the lookups the walk needs from the index: the entry of
// each path, and the directories a tracked entry lives under. The latter is
// only consulted to keep a tracked subtree out of an ignored directory, so it
// is built only when an ignore scope is in use.
//
// A scoped walk keeps only the entries it can reach: the prefix, everything
// under it, and the ancestor paths on the way to it. Building the maps from the
// whole index would make setup time and memory proportional to the index rather
// than to the subtree, which is the cost the scope exists to remove. Dropping
// an unrelated sibling changes nothing about the walk because prefixWants
// already refuses every name outside the prefix before it is looked up here.
func indexMaps(idx *index.Index, scope *gitignore.Scope, prefix string) (map[string]*index.Entry, map[string]struct{}) {
	if idx == nil {
		return nil, nil
	}

	// Size the maps for the scoped walk when there is one, so the allocation
	// does not follow the whole index either.
	size := len(idx.Entries)
	if prefix != "" {
		size = 0
		for _, entry := range idx.Entries {
			if withinPrefix(entry.Name, prefix) {
				size++
			}
		}
	}

	idxMap := make(map[string]*index.Entry, size)
	for _, entry := range idx.Entries {
		if !withinPrefix(entry.Name, prefix) {
			continue
		}
		idxMap[entry.Name] = entry
	}

	if scope == nil {
		return idxMap, nil
	}

	trackedDirs := make(map[string]struct{})
	for name := range idxMap {
		for parent := path.Dir(name); parent != "." && parent != "/"; parent = path.Dir(parent) {
			if _, ok := trackedDirs[parent]; ok {
				break
			}
			trackedDirs[parent] = struct{}{}
		}
	}

	return idxMap, trackedDirs
}

// withinPrefix reports whether the entry at path is one a walk scoped to prefix
// can reach: the prefix itself, an entry under it, or a directory on the way to
// it. An empty prefix reaches everything. The last case is what keeps the
// ancestors of a nested prefix, so the scoped walk still lists the directories
// leading to it and their ignore scope still applies to its components.
//
// It mirrors the predicate of the same name on the index noder, which filters
// the other side of the same diff.
func withinPrefix(path, prefix string) bool {
	if prefix == "" {
		return true
	}

	if prefix == path || strings.HasPrefix(path, prefix+"/") {
		return true
	}

	return strings.HasPrefix(prefix, path+"/")
}

// prefixComponents returns the slash-separated components of the walk's
// prefix, or nil when the walk is not scoped to a subtree.
func (n *node) prefixComponents() []string {
	if n.options == nil || n.options.Prefix == "" {
		return nil
	}

	return strings.Split(strings.Trim(n.options.Prefix, "/"), "/")
}

// prefixWants reports whether the entry of n's directory with the given name is
// the component of the walk's prefix at this depth, and so has to be taken or
// descended into. Every entry is wanted when the walk is not scoped, or once the
// walk has reached the prefix and the prefix scopes nothing any more.
//
// The listing is not assumed to be ordered: an entry of an unrelated name is
// skipped wherever it appears, which matters because the billy filesystem used on
// disk returns entries in readdir order.
func (n *node) prefixWants(name string) bool {
	prefix := n.prefixComponents()
	if prefix == nil {
		return true
	}

	components := n.pathComponents()
	if len(components) >= len(prefix) {
		return true
	}

	return name == prefix[len(components)]
}

// scopedChildren reports whether all the children of n are outside the prefix.
// It holds for a directory on the way to the prefix that is past the directory
// the prefix names: no path below it begins with the prefix, so the walk stops
// rather than descending into it.
func (n *node) scopedChildren() bool {
	prefix := n.prefixComponents()
	if prefix == nil {
		return false
	}

	components := n.pathComponents()
	if len(components) == 0 || len(components) >= len(prefix) {
		return false
	}

	return components[len(components)-1] != prefix[len(components)-1]
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

	if n.scopedChildren() {
		return nil
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

		if !n.prefixWants(file.Name()) {
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

	if n.scope == nil {
		return nil
	}

	var readOwn func() ([]gitignore.Pattern, error)
	for _, f := range files {
		if f.Name() == gitignore.IgnoreFile && !f.IsDir() {
			dir := n.pathComponents()
			readOwn = func() ([]gitignore.Pattern, error) {
				return gitignore.DirPatterns(n.fs, dir)
			}
			break
		}
	}

	scope, err := n.scope.Descend(n.pathComponents(), readOwn)
	if err != nil {
		return err
	}
	n.scope = scope

	return nil
}

func (n *node) pathComponents() []string {
	if n.path == "" {
		return nil
	}
	return strings.Split(n.path, "/")
}

// shouldSkipIgnored reports whether the child entry of n with the given
// name should be skipped because it matches the ignore scope in effect
// AND has no entry in the index. Tracked entries are never skipped so
// modifications to them are still reported.
func (n *node) shouldSkipIgnored(name string, isDir bool) bool {
	if n.options == nil || n.options.IgnoreScope == nil {
		return false
	}
	// Without an index we cannot prove that a subtree contains no tracked
	// entries, so refuse to skip. This matches the documented contract on
	// Options.IgnoreScope.
	if n.idxMap == nil {
		return false
	}
	childPath := path.Join(n.path, name)
	if !n.scope.Match(strings.Split(childPath, "/"), isDir) {
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

func (n *node) newChildNode(file os.FileInfo) (*node, error) {
	path := path.Join(n.path, file.Name())

	node := &node{
		fs:          n.fs,
		submodules:  n.submodules,
		idx:         n.idx,
		idxMap:      n.idxMap,
		trackedDirs: n.trackedDirs,
		options:     n.options,

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
