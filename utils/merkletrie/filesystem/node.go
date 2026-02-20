// Package filesystem provides a merkletrie noder implementation for billy filesystems.
package filesystem

import (
	"io"
	"os"
	"path"
	"time"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
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

	options *Options

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

	if options.Index != nil {
		idxMap = make(map[string]*index.Entry, len(options.Index.Entries))
		for _, entry := range options.Index.Entries {
			idxMap[entry.Name] = entry
		}
	}

	return &node{
		fs:         fs,
		submodules: submodules,
		idx:        options.Index,
		idxMap:     idxMap,
		options:    &options,
		isDir:      true,
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

		c, err := n.newChildNode(fi)
		if err != nil {
			return err
		}

		n.children = append(n.children, c)
	}

	return nil
}

func (n *node) newChildNode(file os.FileInfo) (*node, error) {
	path := path.Join(n.path, file.Name())

	node := &node{
		fs:         n.fs,
		submodules: n.submodules,
		idx:        n.idx,
		idxMap:     n.idxMap,
		options:    n.options,

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
