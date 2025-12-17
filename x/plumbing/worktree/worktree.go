package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6"
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
)

var worktreeNameRE = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)

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

	if !worktreeNameRE.MatchString(name) {
		return fmt.Errorf("invalid worktree name %q", name)
	}

	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	err := o.Validate()
	if err != nil {
		return err
	}

	commonDir := w.storer.Filesystem()
	err = w.addDotGitDirs(commonDir, name)
	if err != nil {
		return err
	}

	err = w.addDotGitFiles(commonDir, wt, name, o)
	if err != nil {
		return err
	}

	path := filepath.Join(commonDir.Root(), worktrees, name)
	err = w.addWorktreeDotGitFile(wt, path)
	if err != nil {
		return err
	}

	r, err := w.Open(wt)
	if err != nil {
		return err
	}

	work, err := r.Worktree()
	if err != nil {
		return err
	}

	return work.Reset(&git.ResetOptions{Commit: o.commit, Mode: git.HardReset})
}

// Remove deletes a linked worktree by removing its metadata dir within .git.
//
// This method removes the metadata directory for the specified worktree from
// .git/worktrees/<name>, similar to the `git worktree remove` command. Note
// that this only removes the metadata; it does not delete the actual worktree
// filesystem or its files.
func (w *Worktree) Remove(name string) error {
	if !worktreeNameRE.MatchString(name) {
		return fmt.Errorf("invalid worktree name %q", name)
	}

	dotgit := w.storer.Filesystem()
	path := filepath.Join(dotgit.Root(), worktrees, name)
	fi, err := dotgit.Lstat(path)
	if err != nil {
		return err
	}

	if !fi.IsDir() {
		return errors.New("invalid worktree")
	}

	return util.RemoveAll(dotgit, path)
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
	return git.Open(stor, wt)
}

func (w *Worktree) getDualFS(wt billy.Filesystem) billy.Filesystem {
	commonDir := w.storer.Filesystem()

	f, err := wt.Open(dotgitDir)
	if err != nil {
		return nil
	}

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
