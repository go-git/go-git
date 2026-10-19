package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

const (
	// names for dir and files managed by worktrees.
	dotgitDir    = ".git"
	worktrees    = "worktrees"
	commonDir    = "commondir"
	gitDir       = "gitdir"
	head         = "HEAD"
	originalHead = "ORIG_HEAD"
	refs         = "refs"

	dirMode               = 0o777
	worktreeDotGitMaxSize = 1024

	// maxWorktreeNameLen bounds the length of a worktree name. The name is a
	// directory entry under .git/worktrees, so it cannot be longer than one
	// path component: that limit is 255 on the filesystems go-git runs on —
	// counted in bytes, characters or UTF-16 units depending on which — and
	// git itself fails a longer name with ENAMETOOLONG. A name that passes
	// worktreeNameRE is ASCII, so all three counts agree and the limit means
	// the same thing under each. Rejecting up front keeps a name that can
	// never become a worktree out of a path, a regexp and an error message.
	maxWorktreeNameLen = 255

	// lockSuffix is what git appends to a loose reference while updating it.
	lockSuffix = ".lock"

	// maxBranchNameLen bounds a worktree name that Add also creates a branch
	// for. Git updates refs/heads/<name> through a sibling <name>.lock, so the
	// longest branch git can carry is lockSuffix shorter than the longest
	// directory entry it can create. go-git writes the reference in place and
	// would take the longer name happily, leaving a worktree only go-git can
	// use: git refuses to check it out or move its branch, and says so only
	// once someone tries. Add rejects the name instead.
	maxBranchNameLen = maxWorktreeNameLen - len(lockSuffix)

	// maxQuotedLen limits how much of a rejected name an error quotes back.
	// Quoting expands a byte up to fourfold, so quoting a name whole makes
	// the message several times its size.
	maxQuotedLen = 64
)

var (
	worktreeNameRE = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)

	// ErrWorktreeNotFound is returned when attempting to remove a worktree that does not exist.
	ErrWorktreeNotFound = errors.New("worktree not found")

	// ErrWorktreeAlreadyExists is returned when attempting to add a worktree with a name that already exists.
	ErrWorktreeAlreadyExists = errors.New("worktree already exists")

	// ErrInvalidWorktreeName is returned when a worktree name is too long or
	// holds a byte a name cannot hold. The wrapping error says which.
	ErrInvalidWorktreeName = errors.New("invalid worktree name")
)

// Worktree manages multiple working trees attached to a git repository.
// It provides functionality to add and remove linked worktrees, allowing
// multiple branches to be checked out simultaneously in different directories.
//
// A Worktree instance is tied to a specific repository through its storage
// backend, which must implement the WorktreeStorer interface.
type Worktree struct {
	storer xstorage.WorktreeStorer
}

// New creates a new Worktree manager for the given storage backend.
//
// The storer must implement the WorktreeStorer interface, which provides
// access to the repository's filesystem for managing worktree metadata.
//
// Returns an error if storer is nil or does not implement WorktreeStorer.
func New(storer storage.Storer) (*Worktree, error) {
	if storer == nil {
		return nil, errors.New("storer is nil")
	}

	wts, ok := storer.(xstorage.WorktreeStorer)
	if !ok {
		return nil, errors.New("storer does not implement WorktreeStorer")
	}

	return &Worktree{
		storer: wts,
	}, nil
}

// validateName checks that name can be used as a worktree name, no longer
// than maxLen: maxWorktreeNameLen for a name that is only a directory entry,
// maxBranchNameLen for one that becomes a branch too. Length is checked
// before shape, so an over-long name is reported by its length instead of
// being quoted back whole.
func validateName(name string, maxLen int) error {
	if len(name) > maxLen {
		return fmt.Errorf("%w: %d bytes exceeds the %d byte limit",
			ErrInvalidWorktreeName, len(name), maxLen)
	}

	if !worktreeNameRE.MatchString(name) {
		return fmt.Errorf("%w %s", ErrInvalidWorktreeName, quoteBounded(name))
	}

	return nil
}

// quoteBounded returns name quoted for an error message, truncated to
// maxQuotedLen bytes.
func quoteBounded(name string) string {
	if len(name) <= maxQuotedLen {
		return strconv.Quote(name)
	}

	return strconv.Quote(name[:maxQuotedLen]) + "..."
}

// Add creates a new linked worktree with the specified name and filesystem.
//
// This method sets up the necessary metadata and directory structure for a new
// worktree, similar to the `git worktree add` command. The worktree will be
// associated with the repository and can be used to work on a different commit
// or branch simultaneously.
func (w *Worktree) Add(wt billy.Filesystem, name string, opts ...Option) error {
	if wt == nil {
		return errors.New("cannot add worktree: fs is nil")
	}

	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	// An attached worktree takes its name as a branch name as well, which
	// git bounds shorter than the directory entry under .git/worktrees.
	maxLen := maxWorktreeNameLen
	if !o.detachedHead {
		maxLen = maxBranchNameLen
	}

	if err := validateName(name, maxLen); err != nil {
		return err
	}

	if o.commit.IsZero() {
		r, err := git.Open(w.storer.(storage.Storer), nil)
		if err != nil {
			return fmt.Errorf("unable to open repository: %w", err)
		}
		defer func() {
			r.Storer = nil // avoid closing the storer, which is shared with the worktree
			_ = r.Close()
		}()

		ref, err := r.Head()
		if err != nil {
			return fmt.Errorf("invalid reference: %w", err)
		}
		o.commit = ref.Hash()
	}

	err := o.Validate()
	if err != nil {
		return err
	}

	commonDir := w.storer.Filesystem()

	path := filepath.Join(commonDir.Root(), worktrees, name)
	_, err = commonDir.Lstat(path)
	if err == nil {
		return ErrWorktreeAlreadyExists
	}

	err = w.addDotGitDirs(commonDir, name)
	if err != nil {
		return err
	}

	err = w.addDotGitFiles(commonDir, wt, name, o)
	if err != nil {
		return err
	}

	err = w.addWorktreeDotGitFile(wt, path)
	if err != nil {
		return err
	}

	r, err := w.Open(wt)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	work, err := r.Worktree()
	if err != nil {
		return err
	}
	opt := &git.CheckoutOptions{
		Hash: o.commit,
	}

	if !o.detachedHead {
		opt.Branch = plumbing.NewBranchReferenceName(name)
		opt.Create = true
	}

	return work.Checkout(opt)
}

// Remove deletes a linked worktree by removing its metadata dir within .git.
//
// This method removes the metadata directory for the specified worktree from
// .git/worktrees/<name>, similar to the `git worktree remove` command. Note
// that this only removes the metadata; it does not delete the actual worktree
// filesystem or its files.
func (w *Worktree) Remove(name string) error {
	if err := validateName(name, maxWorktreeNameLen); err != nil {
		return err
	}

	dotgit := w.storer.Filesystem()
	path := filepath.Join(dotgit.Root(), worktrees, name)
	fi, err := dotgit.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrWorktreeNotFound
		}
		return err
	}

	if !fi.IsDir() {
		return errors.New("invalid worktree")
	}

	return util.RemoveAll(dotgit, path)
}

// List returns a list of all linked worktree names.
func (w *Worktree) List() ([]string, error) {
	dotgit := w.storer.Filesystem()

	_, err := dotgit.Lstat(worktrees)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}

	entries, err := dotgit.ReadDir(worktrees)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}

	return names, nil
}

// Open opens a repository that may be a linked worktree.
//
// When the target is not a linked worktree, it behaves just like git.Open.
// This logic is likely going to be moved to git.Open in the future.
func (w *Worktree) Open(wt billy.Filesystem) (*git.Repository, error) {
	if wt == nil {
		return nil, errors.New("worktree fs is nil")
	}

	fs := w.getDualFS(wt)
	if fs == nil {
		fs = w.storer.Filesystem()
	}

	stor := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	repo, err := git.Open(stor, wt)
	if err != nil {
		_ = stor.Close()
		return nil, err
	}
	return repo, nil
}

// Init initialises a worktree filesystem, connecting it to an existing
// worktree metadata.
//
// This is a go-git concept, which adds flexibility to the way linked
// worktrees work. It enables a fs to be connected to an existing metadata,
// which is particularly useful cross-filesystem implementations.
// For example, in-memory worktrees that are connected pre-existing worktree
// metadata on disk - or vice versa.
func (w *Worktree) Init(wt billy.Filesystem, name string) error {
	if wt == nil {
		return errors.New("worktree fs is nil")
	}

	if err := validateName(name, maxWorktreeNameLen); err != nil {
		return err
	}

	commonDir := w.storer.Filesystem()
	path := filepath.Join(commonDir.Root(), worktrees, name)

	_, err := commonDir.Lstat(path)
	if err != nil {
		return ErrWorktreeNotFound
	}

	err = w.addWorktreeDotGitFile(wt, path)
	if err != nil {
		return fmt.Errorf("unable to create .git file: %w", err)
	}

	fs := w.getDualFS(wt)
	if fs == nil {
		return errors.New("unable to generate dual fs")
	}

	return nil
}

func (w *Worktree) getDualFS(wt billy.Filesystem) billy.Filesystem {
	commonDir := w.storer.Filesystem()

	f, err := wt.Open(dotgitDir)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, worktreeDotGitMaxSize))
	if err != nil || len(data) < 9 {
		return nil
	}

	// ensure it is reading gitdir data:
	if !bytes.Equal(data[:len(gitDir)], []byte(gitDir)) {
		return nil
	}

	path := strings.TrimSpace(string(data[8:]))
	rel, err := filepath.Rel(commonDir.Root(), path)
	if err != nil {
		return nil
	}

	wtGitDir, err := commonDir.Chroot(rel)
	if err != nil {
		return nil
	}

	return dotgit.NewRepositoryFilesystem(wtGitDir, commonDir)
}

func (w *Worktree) addDotGitDirs(wt billy.Filesystem, name string) error {
	return wt.MkdirAll(path(name, refs), dirMode)
}

func (w *Worktree) addWorktreeDotGitFile(wt billy.Filesystem, path string) error {
	return writeFile(wt, dotgitDir, []byte("gitdir: "+path))
}

func (w *Worktree) addDotGitFiles(dotgit, wt billy.Filesystem, name string, opts *options) error {
	err := writeFile(dotgit, path(name, commonDir), []byte("../.."))
	if err != nil {
		return err
	}

	err = writeFile(dotgit, path(name, gitDir), []byte(filepath.Join(wt.Root(), ".git")))
	if err != nil {
		return err
	}

	err = writeFile(dotgit, path(name, head), []byte(opts.commit.String()))
	if err != nil {
		return err
	}

	return writeFile(dotgit, path(name, originalHead), []byte(opts.commit.String()))
}

func writeFile(wt billy.Filesystem, fn string, data []byte) (err error) {
	var f billy.File
	f, err = wt.Create(fn)
	if err != nil {
		return err
	}

	defer func() {
		err = f.Close()
	}()

	_, err = f.Write(append(data, []byte("\n")...))

	return err
}

func path(wtn, fn string) string {
	return filepath.Join(worktrees, wtn, fn)
}
