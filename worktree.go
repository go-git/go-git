package git

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/plumbing/format/index"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie"

	"gopkg.in/src-d/go-billy.v2"
)

var (
	ErrWorktreeNotClean  = errors.New("worktree is not clean")
	ErrSubmoduleNotFound = errors.New("submodule not found")
	ErrUnstaggedChanges  = errors.New("worktree contains unstagged changes")
)

// Worktree represents a git worktree.
type Worktree struct {
	r  *Repository
	fs billy.Filesystem
}

// Checkout switch branches or restore working tree files.
func (w *Worktree) Checkout(opts *CheckoutOptions) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	if !opts.Force {
		unstaged, err := w.cointainsUnstagedChanges()
		if err != nil {
			return err
		}

		if unstaged {
			return ErrUnstaggedChanges
		}
	}

	c, err := w.getCommitFromCheckoutOptions(opts)
	if err != nil {
		return err
	}

	ro := &ResetOptions{Commit: c, Mode: MergeReset}
	if opts.Force {
		ro.Mode = HardReset
	}

	if !opts.Hash.IsZero() {
		err = w.setHEADToCommit(opts.Hash)
	} else {
		err = w.setHEADToBranch(opts.Branch, c)
	}

	if err != nil {
		return err
	}

	return w.Reset(ro)
}

func (w *Worktree) getCommitFromCheckoutOptions(opts *CheckoutOptions) (plumbing.Hash, error) {
	if !opts.Hash.IsZero() {
		return opts.Hash, nil
	}

	b, err := w.r.Reference(opts.Branch, true)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	if !b.IsTag() {
		return b.Hash(), nil
	}

	o, err := w.r.Object(plumbing.AnyObject, b.Hash())
	if err != nil {
		return plumbing.ZeroHash, err
	}

	switch o := o.(type) {
	case *object.Tag:
		if o.TargetType != plumbing.CommitObject {
			return plumbing.ZeroHash, fmt.Errorf("unsupported tag object target %q", o.TargetType)
		}

		return o.Target, nil
	case *object.Commit:
		return o.Hash, nil
	}

	return plumbing.ZeroHash, fmt.Errorf("unsupported tag target %q", o.Type())
}

func (w *Worktree) setHEADToCommit(commit plumbing.Hash) error {
	head := plumbing.NewHashReference(plumbing.HEAD, commit)
	return w.r.Storer.SetReference(head)
}

func (w *Worktree) setHEADToBranch(branch plumbing.ReferenceName, commit plumbing.Hash) error {
	target, err := w.r.Storer.Reference(branch)
	if err != nil {
		return err
	}

	var head *plumbing.Reference
	if target.IsBranch() {
		head = plumbing.NewSymbolicReference(plumbing.HEAD, target.Name())
	} else {
		head = plumbing.NewHashReference(plumbing.HEAD, commit)
	}

	return w.r.Storer.SetReference(head)
}

// Reset the worktree to a specified state.
func (w *Worktree) Reset(opts *ResetOptions) error {
	if err := opts.Validate(w.r); err != nil {
		return err
	}

	if opts.Mode == MergeReset {
		unstaged, err := w.cointainsUnstagedChanges()
		if err != nil {
			return err
		}

		if unstaged {
			return ErrUnstaggedChanges
		}
	}

	changes, err := w.diffCommitWithStaging(opts.Commit, true)
	if err != nil {
		return err
	}

	idx, err := w.r.Storer.Index()
	if err != nil {
		return err
	}

	t, err := w.getTreeFromCommitHash(opts.Commit)
	if err != nil {
		return err
	}

	for _, ch := range changes {
		if err := w.checkoutChange(ch, t, idx); err != nil {
			return err
		}
	}

	if err := w.r.Storer.SetIndex(idx); err != nil {
		return err
	}

	return w.setHEADCommit(opts.Commit)
}

func (w *Worktree) cointainsUnstagedChanges() (bool, error) {
	ch, err := w.diffStagingWithWorktree()
	if err != nil {
		return false, err
	}

	return len(ch) != 0, nil
}

func (w *Worktree) setHEADCommit(commit plumbing.Hash) error {
	head, err := w.r.Reference(plumbing.HEAD, false)
	if err != nil {
		return err
	}

	if head.Type() == plumbing.HashReference {
		head = plumbing.NewHashReference(plumbing.HEAD, commit)
		return w.r.Storer.SetReference(head)
	}

	branch, err := w.r.Reference(head.Target(), false)
	if err != nil {
		return err
	}

	if !branch.IsBranch() {
		return fmt.Errorf("invalid HEAD target should be a branch, found %s", branch.Type())
	}

	branch = plumbing.NewHashReference(branch.Name(), commit)
	return w.r.Storer.SetReference(branch)
}

func (w *Worktree) checkoutChange(ch merkletrie.Change, t *object.Tree, idx *index.Index) error {
	a, err := ch.Action()
	if err != nil {
		return err
	}

	switch a {
	case merkletrie.Modify:
		name := ch.To.String()
		if err := w.rmIndexFromFile(name, idx); err != nil {
			return err
		}

		// to apply perm changes the file is deleted, billy doesn't implement
		// chmod
		if err := w.fs.Remove(name); err != nil {
			return err
		}

		fallthrough
	case merkletrie.Insert:
		name := ch.To.String()
		e, err := t.FindEntry(name)
		if err != nil {
			return err
		}

		if e.Mode == filemode.Submodule {
			return w.addIndexFromTreeEntry(name, e, idx)
		}

		f, err := t.File(name)
		if err != nil {
			return err
		}

		if err := w.checkoutFile(f); err != nil {
			return err
		}

		return w.addIndexFromFile(name, e.Hash, idx)
	case merkletrie.Delete:
		name := ch.From.String()
		if err := w.fs.Remove(name); err != nil {
			return err
		}

		return w.rmIndexFromFile(name, idx)
	}

	return nil
}

func (w *Worktree) checkoutFile(f *object.File) error {
	from, err := f.Reader()
	if err != nil {
		return err
	}
	defer from.Close()

	mode, err := f.Mode.ToOSFileMode()
	if err != nil {
		return err
	}

	to, err := w.fs.OpenFile(f.Name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer to.Close()

	if _, err := io.Copy(to, from); err != nil {
		return err
	}

	return err
}

func (w *Worktree) addIndexFromTreeEntry(name string, f *object.TreeEntry, idx *index.Index) error {
	idx.Entries = append(idx.Entries, index.Entry{
		Hash: f.Hash,
		Name: name,
		Mode: filemode.Submodule,
	})

	return nil
}

func (w *Worktree) addIndexFromFile(name string, h plumbing.Hash, idx *index.Index) error {
	fi, err := w.fs.Stat(name)
	if err != nil {
		return err
	}

	mode, err := filemode.NewFromOSFileMode(fi.Mode())
	if err != nil {
		return err
	}

	e := index.Entry{
		Hash:       h,
		Name:       name,
		Mode:       mode,
		ModifiedAt: fi.ModTime(),
		Size:       uint32(fi.Size()),
	}

	// if the FileInfo.Sys() comes from os the ctime, dev, inode, uid and gid
	// can be retrieved, otherwise this doesn't apply
	if fillSystemInfo != nil {
		fillSystemInfo(&e, fi.Sys())
	}

	idx.Entries = append(idx.Entries, e)
	return nil
}

func (w *Worktree) rmIndexFromFile(name string, idx *index.Index) error {
	for i, e := range idx.Entries {
		if e.Name != name {
			continue
		}

		idx.Entries = append(idx.Entries[:i], idx.Entries[i+1:]...)
		return nil
	}

	return nil
}

func (w *Worktree) getTreeFromCommitHash(commit plumbing.Hash) (*object.Tree, error) {
	c, err := w.r.CommitObject(commit)
	if err != nil {
		return nil, err
	}

	return c.Tree()
}

func (w *Worktree) initializeIndex() error {
	return w.r.Storer.SetIndex(&index.Index{Version: 2})
}

var fillSystemInfo func(e *index.Entry, sys interface{})

const gitmodulesFile = ".gitmodules"

// Submodule returns the submodule with the given name
func (w *Worktree) Submodule(name string) (*Submodule, error) {
	l, err := w.Submodules()
	if err != nil {
		return nil, err
	}

	for _, m := range l {
		if m.Config().Name == name {
			return m, nil
		}
	}

	return nil, ErrSubmoduleNotFound
}

// Submodules returns all the available submodules
func (w *Worktree) Submodules() (Submodules, error) {
	l := make(Submodules, 0)
	m, err := w.readGitmodulesFile()
	if err != nil || m == nil {
		return l, err
	}

	c, err := w.r.Config()
	for _, s := range m.Submodules {
		l = append(l, w.newSubmodule(s, c.Submodules[s.Name]))
	}

	return l, nil
}

func (w *Worktree) newSubmodule(fromModules, fromConfig *config.Submodule) *Submodule {
	m := &Submodule{w: w}
	m.initialized = fromConfig != nil

	if !m.initialized {
		m.c = fromModules
		return m
	}

	m.c = fromConfig
	m.c.Path = fromModules.Path
	return m
}

func (w *Worktree) readGitmodulesFile() (*config.Modules, error) {
	f, err := w.fs.Open(gitmodulesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	input, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	m := config.NewModules()
	return m, m.Unmarshal(input)
}

func (w *Worktree) readIndexEntry(path string) (index.Entry, error) {
	var e index.Entry

	idx, err := w.r.Storer.Index()
	if err != nil {
		return e, err
	}

	for _, e := range idx.Entries {
		if e.Name == path {
			return e, nil
		}
	}

	return e, fmt.Errorf("unable to find %q entry in the index", path)
}
