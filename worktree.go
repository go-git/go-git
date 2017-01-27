package git

import (
	"io"
	"os"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"

	"srcd.works/go-billy.v1"
)

type Worktree struct {
	r  *Repository
	fs billy.Filesystem
}

func (w *Worktree) Checkout(commit plumbing.Hash) error {
	c, err := w.r.Commit(commit)
	if err != nil {
		return err
	}

	files, err := c.Files()
	if err != nil {
		return err
	}

	return files.ForEach(w.checkoutFile)
}

func (w *Worktree) checkoutFile(f *object.File) error {
	from, err := f.Reader()
	if err != nil {
		return err
	}

	defer from.Close()
	to, err := w.fs.OpenFile(f.Name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(to, from); err != nil {
		return err
	}

	return to.Close()
}
