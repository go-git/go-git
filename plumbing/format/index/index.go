package index

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

var (
	// ErrUnsupportedVersion is returned by Decode when the index file version
	// is not supported.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrEntryNotFound is returned by Index.Entry, if an entry is not found.
	ErrEntryNotFound = errors.New("entry not found")

	indexSignature                    = []byte{'D', 'I', 'R', 'C'} // https://git-scm.com/docs/index-format#_the_git_index_file_has_the_following_format
	treeExtSignature                  = []byte{'T', 'R', 'E', 'E'} // https://git-scm.com/docs/index-format#_cache_tree
	resolveUndoExtSignature           = []byte{'R', 'E', 'U', 'C'} // https://git-scm.com/docs/index-format#_resolve_undo
	linkExtSignature                  = []byte{'l', 'i', 'n', 'k'} // https://git-scm.com/docs/index-format#_split_index
	untrackedCacheExtSignature        = []byte{'U', 'N', 'T', 'R'} // https://git-scm.com/docs/index-format#_untracked_cache
	endOfIndexEntryExtSignature       = []byte{'E', 'O', 'I', 'E'} // https://git-scm.com/docs/index-format#_end_of_index_entry
	fsMonitorExtSignature             = []byte{'F', 'S', 'M', 'N'} // https://git-scm.com/docs/index-format#_file_system_monitor_cache
	indexEntryOffsetTableExtSignature = []byte{'I', 'E', 'O', 'T'} // https://git-scm.com/docs/index-format#_index_entry_offset_table
)

// Stage during merge
type Stage int

const (
	// Merged is the default stage, fully merged
	Merged Stage = 1
	// AncestorMode is the base revision
	AncestorMode Stage = 1
	// OurMode is the first tree revision, ours
	OurMode Stage = 2
	// TheirMode is the second tree revision, theirs
	TheirMode Stage = 3
)

// Index contains the information about which objects are currently checked out
// in the worktree, having information about the working files. Changes in
// worktree are detected using this Index. The Index is also used during merges
type Index struct {
	// Version is index version
	Version uint32
	// Entries collection of entries represented by this Index. The order of
	// this collection is not guaranteed
	Entries []*Entry
	// Cache represents the 'Cache Tree' extension
	Cache *Tree
	// ResolveUndo represents the 'Resolve Undo' extension
	ResolveUndo *ResolveUndo
	// EndOfIndexEntry represents the 'End of Index Entry' extension
	EndOfIndexEntry *EndOfIndexEntry
	// Link represents the 'Split Index' extension
	Link *Link
	// UntrackedCache represents the 'Untracked Cache' extension
	UntrackedCache *UntrackedCache
	// FSMonitor represents the 'File System Monitor Cache' extension
	FSMonitor *FSMonitor
	// IndexEntryOffsetTable represents the 'Index Entry Offset Table' extension
	IndexEntryOffsetTable *IndexEntryOffsetTable
}

// Add creates a new Entry and returns it. The caller should first check that
// another entry with the same path does not exist.
func (i *Index) Add(path string) *Entry {
	e := &Entry{
		Name: filepath.ToSlash(path),
	}

	i.Entries = append(i.Entries, e)
	return e
}

// Entry returns the entry that match the given path, if any.
func (i *Index) Entry(path string) (*Entry, error) {
	path = filepath.ToSlash(path)
	for _, e := range i.Entries {
		if e.Name == path {
			return e, nil
		}
	}

	return nil, ErrEntryNotFound
}

// Remove remove the entry that match the give path and returns deleted entry.
func (i *Index) Remove(path string) (*Entry, error) {
	path = filepath.ToSlash(path)
	for index, e := range i.Entries {
		if e.Name == path {
			i.Entries = append(i.Entries[:index], i.Entries[index+1:]...)
			return e, nil
		}
	}

	return nil, ErrEntryNotFound
}

// Glob returns the all entries matching pattern or nil if there is no matching
// entry. The syntax of patterns is the same as in filepath.Glob.
func (i *Index) Glob(pattern string) (matches []*Entry, err error) {
	pattern = filepath.ToSlash(pattern)
	for _, e := range i.Entries {
		m, err := match(pattern, e.Name)
		if err != nil {
			return nil, err
		}

		if m {
			matches = append(matches, e)
		}
	}

	return
}

// String is equivalent to `git ls-files --stage --debug`
func (i *Index) String() string {
	buf := bytes.NewBuffer(nil)
	for _, e := range i.Entries {
		buf.WriteString(e.String())
	}

	return buf.String()
}

// Entry represents a single file (or stage of a file) in the cache. An entry
// represents exactly one stage of a file. If a file path is unmerged then
// multiple Entry instances may appear for the same path name.
type Entry struct {
	// Hash is the SHA1 of the represented file
	Hash plumbing.Hash
	// Name is the  Entry path name relative to top level directory
	Name string
	// CreatedAt time when the tracked path was created
	CreatedAt time.Time
	// ModifiedAt time when the tracked path was changed
	ModifiedAt time.Time
	// Dev and Inode of the tracked path
	Dev, Inode uint32
	// Mode of the path
	Mode filemode.FileMode
	// UID and GID, userid and group id of the owner
	UID, GID uint32
	// Size is the length in bytes for regular files
	Size uint32
	// Stage on a merge is defines what stage is representing this entry
	// https://git-scm.com/book/en/v2/Git-Tools-Advanced-Merging
	Stage Stage
	// SkipWorktree used in sparse checkouts
	// https://git-scm.com/docs/git-read-tree#_sparse_checkout
	SkipWorktree bool
	// IntentToAdd record only the fact that the path will be added later
	// https://git-scm.com/docs/git-add ("git add -N")
	IntentToAdd bool
}

func (e Entry) String() string {
	buf := bytes.NewBuffer(nil)

	fmt.Fprintf(buf, "%06o %s %d\t%s\n", e.Mode, e.Hash, e.Stage, e.Name)
	fmt.Fprintf(buf, "  ctime: %d:%d\n", e.CreatedAt.Unix(), e.CreatedAt.Nanosecond())
	fmt.Fprintf(buf, "  mtime: %d:%d\n", e.ModifiedAt.Unix(), e.ModifiedAt.Nanosecond())
	fmt.Fprintf(buf, "  dev: %d\tino: %d\n", e.Dev, e.Inode)
	fmt.Fprintf(buf, "  uid: %d\tgid: %d\n", e.UID, e.GID)
	fmt.Fprintf(buf, "  size: %d\tflags: %x\n", e.Size, 0)

	return buf.String()
}

// Tree contains pre-computed hashes for trees that can be derived from the
// index. It helps speed up tree object generation from index for a new commit.
type Tree struct {
	Entries []TreeEntry
}

// TreeEntry entry of a cached Tree
type TreeEntry struct {
	// Path component (relative to its parent directory)
	Path string
	// Entries is the number of entries in the index that is covered by the tree
	// this entry represents.
	Entries int
	// Trees is the number that represents the number of subtrees this tree has
	Trees int
	// Hash object name for the object that would result from writing this span
	// of index as a tree.
	Hash plumbing.Hash
}

// ResolveUndo is used when a conflict is resolved (e.g. with "git add path"),
// these higher stage entries are removed and a stage-0 entry with proper
// resolution is added. When these higher stage entries are removed, they are
// saved in the resolve undo extension.
type ResolveUndo struct {
	Entries []ResolveUndoEntry
}

// ResolveUndoEntry contains the information about a conflict when is resolved
type ResolveUndoEntry struct {
	Path   string
	Stages map[Stage]plumbing.Hash
}

// EndOfIndexEntry is the End of Index Entry (EOIE) is used to locate the end of
// the variable length index entries and the beginning of the extensions. Code
// can take advantage of this to quickly locate the index extensions without
// having to parse through all of the index entries.
//
//	Because it must be able to be loaded before the variable length cache
//	entries and other index extensions, this extension must be written last.
type EndOfIndexEntry struct {
	// Offset to the end of the index entries
	Offset uint32
	// Hash is a SHA-1 over the extension types and their sizes (but not
	//	their contents).
	Hash plumbing.Hash
}

// SkipUnless applies patterns in the form of A, A/B, A/B/C
// to the index to prevent the files from being checked out
func (i *Index) SkipUnless(patterns []string) {
	for _, e := range i.Entries {
		var include bool
		for _, pattern := range patterns {
			if strings.HasPrefix(e.Name, pattern) {
				include = true
				break
			}
		}
		if !include {
			e.SkipWorktree = true
		}
	}
}

// Link represents the 'LINK' index extension.
//
// This extension is used in split-index mode to overlay modifications on top
// of an immutable shared index file.
type Link struct {
	// ObjectID is the hash of the shared base index. If all-zero, no shared
	// index is used.
	ObjectID plumbing.Hash

	// DeleteBitmap is an EWAH-compressed bitmap representing which entries in
	// the base index are deleted in this overlay.
	//
	// Note, because a delete operation changes index entry positions, but we
	// do need original positions in replace phase, it’s best to just mark
	// entries for removal, then do a mass deletion after replacement.
	DeleteBitmap []byte

	// ReplaceBitmap is an EWAH-compressed bitmap representing which entries in
	// the base index are replaced by the overlay.
	//
	// All replaced entries are stored in sorted order in this index. The first
	// "1" bit in the replace bitmap corresponds to the first index entry, the
	// second "1" bit to the second entry and so on.
	//
	// Replaced entries may have empty path names to save space.
	//
	// The remaining index entries after replaced ones will be added to the
	// final index. These added entries are also sorted by entry name then
	// stage.
	ReplaceBitmap []byte
}

// UntrackedCache represents the 'UNTR' index extension.
//
// This extension is used to avoid full-directory scans by caching the
// environment and directory state related to untracked files.
type UntrackedCache struct {
	// Environments is a sequence of strings describing the environment where the cache is valid.
	Environments []string

	// InfoExcludeCache contains stat metadata for $GIT_DIR/info/exclude.
	// If the file does not exist, these fields are zeroed.
	InfoExcludeStats UntrackedCacheStats

	// ExcludesFileCache contains stat metadata for the file specified by core.excludesFile.
	// If the file does not exist, these fields are zeroed.
	ExcludesFileStats UntrackedCacheStats

	// DirFlags corresponds to struct dir_struct.flags (32-bit).
	// For example, 0x04 means DIR_SHOW_IGNORED_TOO.
	DirFlags uint32

	// InfoExcludeHash is the hash of the contents of $GIT_DIR/info/exclude.
	// All-zero (null hash) if the file does not exist.
	InfoExcludeHash plumbing.Hash

	// ExcludesFileHash is the hash of the contents of the core.excludesFile.
	// All-zero (null hash) if the file does not exist.
	ExcludesFileHash plumbing.Hash

	// PerDirIgnoreFile is a string naming the per-directory ignore file.
	// Typically, ".gitignore".
	PerDirIgnoreFile string

	// Entries is the directory block list in depth-first order.
	Entries []UntrackedCacheEntry

	// ValidBitmap marks directories with valid untracked cache data.
	ValidBitmap []byte

	// CheckOnlyBitmap marks directories to check without recursion.
	CheckOnlyBitmap []byte

	// MetadataBitmap marks directories with valid metadata for ignore files.
	MetadataBitmap []byte

	// Stats holds stat info for per-directory ignore files, aligned with Hashes.
	Stats []UntrackedCacheStats

	// Hashes holds hashes of per-directory ignore files, aligned with Stats.
	Hashes []plumbing.Hash
}

// UntrackedCacheEntry is a directory block in depth-first order.
type UntrackedCacheEntry struct {
	// Blocks is the number of immediate sub-directory blocks.
	Blocks int64

	// Name is the relative directory name, or "" for root.
	Name string

	// Entries lists untracked file and subdirectory names.
	Entries []string
}

// UntrackedCacheStats stores file or directory metadata used for cache validation.
type UntrackedCacheStats struct {
	// CreatedAt corresponds to ctime (create time).
	CreatedAt time.Time

	// ModifiedAt corresponds to mtime (modification time).
	ModifiedAt time.Time

	// Dev and Inode identify the filesystem and node.
	Dev, Inode uint32

	// UID and GID specify the file owner user and group IDs.
	UID, GID uint32

	// Size is the file size in bytes. For directories, this is typically zero.
	Size uint32
}

// FSMonitor represents the 'FSMN' index extension.
//
// It tracks filesystem changes since the last index update to avoid
// unnecessary full-index scans.
type FSMonitor struct {
	// Version of the extension [1, 2].
	Version uint32

	// Since is the timestamp of the last fsmonitor query. This field is only
	// present and valid in version 1 of the extension.
	Since time.Time

	// Token is an opaque string provided by the filesystem monitor. It
	// identifies the last query position in the monitor’s event stream. This
	// field is only present and valid in version 2 of the extension.
	Token string

	// DirtyBitmap is a bitmap of index entries that are known to be dirty. Git
	// uses this to mark which paths must still be re-validated even if the
	// fsmonitor indicates no changes.
	DirtyBitmap []byte
}

// IndexEntryOffsetTable represents the 'IEOT' index extension.
//
// It stores offsets and counts of index entries to enable efficient
// multi-threaded loading.
type IndexEntryOffsetTable struct {
	// Version of the extension (currently, only version 1 is supported).
	Version uint32

	// Entries lists the offset and count of cache entries in blocks.
	Entries []IndexEntryOffsetEntry
}

// IndexEntryOffsetEntry represents an entry in the Index Entry Offset Table.
type IndexEntryOffsetEntry struct {
	// Offset is the byte offset to the first cache entry in this block.
	Offset uint32

	// Count is the number of cache entries in this block.
	Count uint32
}
