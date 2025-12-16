package worktree

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/storage"
	xstorage "github.com/go-git/go-git/v6/x/storage"
)

const (
	// names for dir and files managed by worktrees.
	dotgit       = ".git"
	worktrees    = "worktrees"
	commonDir    = "commondir"
	gitDir       = "gitdir"
	head         = "HEAD"
	originalHead = "ORIG_HEAD"
	index        = "index"
	refs         = "refs"

	dirMode = 0o777
)

var worktreeNameRE = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)

type worktree struct {
	storer xstorage.WorktreeStorer
}

func New(storer storage.Storer) (*worktree, error) {
	if storer == nil {
		return nil, errors.New("storer is nil")
	}

	wts, ok := storer.(xstorage.WorktreeStorer)
	if !ok {
		return nil, errors.New("storer does not implement WorktreeStorer")
	}

	return &worktree{
		storer: wts,
	}, nil
}

func (w *worktree) Add(wt billy.Filesystem, name string, opts ...Option) error {
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

	dotgit := w.storer.Filesystem()
	err = w.addDotGitDirs(dotgit, name)
	if err != nil {
		return err
	}

	err = w.addDotGitFiles(dotgit, wt, name, o)
	if err != nil {
		return err
	}

	path := filepath.Join(dotgit.Root(), worktrees, name)
	err = w.addWorktreeDotGitFile(wt, path)
	if err != nil {
		return err
	}

	r, err := git.Open(w.storer.(storage.Storer), wt)
	if err != nil {
		return err
	}

	work, err := r.Worktree()
	if err != nil {
		return err
	}

	return work.Reset(&git.ResetOptions{Commit: o.commit})
}

func (w *worktree) addDotGitDirs(wt billy.Filesystem, name string) error {
	return wt.MkdirAll(path(name, refs), dirMode)
}

func (w *worktree) addWorktreeDotGitFile(wt billy.Filesystem, path string) error {
	return writeFile(wt, dotgit, []byte("gitdir: "+path))
}

func (w *worktree) addDotGitFiles(dotgit, wt billy.Filesystem, name string, opts *options) error {
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
		return
	}

	defer func() {
		err = f.Close()
	}()

	_, err = f.Write(append(data, []byte("\n")...))

	return
}

func path(wtn, fn string) string {
	return filepath.Join(worktrees, wtn, fn)
}
