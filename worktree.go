package git

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"srcd.works/go-git.v4/plumbing"
	"srcd.works/go-git.v4/plumbing/format/index"
	"srcd.works/go-git.v4/plumbing/object"

	"srcd.works/go-billy.v1"
)

var ErrWorktreeNotClean = errors.New("worktree is not clean")

type Worktree struct {
	r  *Repository
	fs billy.Filesystem
}

func (w *Worktree) Checkout(commit plumbing.Hash) error {
	s, err := w.Status()
	if err != nil {
		return err
	}

	if !s.IsClean() {
		return ErrWorktreeNotClean
	}

	c, err := w.r.Commit(commit)
	if err != nil {
		return err
	}

	files, err := c.Files()
	if err != nil {
		return err
	}

	idx := &index.Index{Version: 2}
	if err := files.ForEach(func(f *object.File) error {
		return w.checkoutFile(f, idx)
	}); err != nil {
		return err
	}

	return w.r.s.SetIndex(idx)
}

func (w *Worktree) checkoutFile(f *object.File, idx *index.Index) error {
	from, err := f.Reader()
	if err != nil {
		return err
	}

	defer from.Close()
	to, err := w.fs.OpenFile(f.Name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode.Perm())
	if err != nil {
		return err
	}

	if _, err := io.Copy(to, from); err != nil {
		return err
	}

	defer to.Close()
	return w.indexFile(f, idx)
}

var fillSystemInfo func(e *index.Entry, os *syscall.Stat_t)

func (w *Worktree) indexFile(f *object.File, idx *index.Index) error {
	fi, err := w.fs.Stat(f.Name)
	if err != nil {
		return err
	}

	e := index.Entry{
		Hash:       f.Hash,
		Name:       f.Name,
		Mode:       w.getMode(fi),
		ModifiedAt: fi.ModTime(),
		Size:       uint32(fi.Size()),
	}

	// if the FileInfo.Sys() comes from os the ctime, dev, inode, uid and gid
	// can be retrieved, otherwise this doesn't apply
	os, ok := fi.Sys().(*syscall.Stat_t)
	if ok && fillSystemInfo != nil {
		fillSystemInfo(&e, os)
	}

	idx.Entries = append(idx.Entries, e)
	return nil
}

func (w *Worktree) Status() (Status, error) {
	idx, err := w.r.s.Index()
	if err != nil {
		return nil, err
	}

	files, err := readDirAll(w.fs)
	if err != nil {
		return nil, err
	}

	s := make(Status, 0)
	for _, e := range idx.Entries {
		fi, ok := files[e.Name]
		delete(files, e.Name)

		if !ok {
			s.File(e.Name).Worktree = Deleted
			continue
		}

		status, err := w.compareFileWithEntry(fi, &e)
		if err != nil {
			return nil, err
		}

		s.File(e.Name).Worktree = status
	}

	for f := range files {
		s.File(f).Worktree = Untracked
	}

	return s, nil
}

func (w *Worktree) compareFileWithEntry(fi billy.FileInfo, e *index.Entry) (StatusCode, error) {
	if fi.Size() != int64(e.Size) {
		return Modified, nil
	}

	if w.getMode(fi) != e.Mode {
		return Modified, nil
	}

	h, err := calcSHA1(w.fs, e.Name)
	if h != e.Hash || err != nil {
		return Modified, err

	}

	return Unmodified, nil
}

func (w *Worktree) getMode(fi billy.FileInfo) os.FileMode {
	if fi.Mode().IsDir() {
		return object.TreeMode
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		return object.SymlinkMode
	}

	const modeExec = 0111
	if fi.Mode()&modeExec != 0 {
		return object.ExecutableMode
	}

	return object.FileMode
}

// Status current status of a Worktree
type Status map[string]*FileStatus

func (s Status) File(filename string) *FileStatus {
	if _, ok := (s)[filename]; !ok {
		s[filename] = &FileStatus{}
	}

	return s[filename]

}

func (s Status) IsClean() bool {
	for _, status := range s {
		if status.Worktree != Unmodified || status.Staging != Unmodified {
			return false
		}
	}

	return true
}

func (s Status) String() string {
	var names []string
	for name := range s {
		names = append(names, name)
	}

	var output string
	for _, name := range names {
		status := s[name]
		if status.Staging == 0 && status.Worktree == 0 {
			continue
		}

		if status.Staging == Renamed {
			name = fmt.Sprintf("%s -> %s", name, status.Extra)
		}

		output += fmt.Sprintf("%s%s %s\n", status.Staging, status.Worktree, name)
	}

	return output
}

// FileStatus status of a file in the Worktree
type FileStatus struct {
	Staging  StatusCode
	Worktree StatusCode
	Extra    string
}

// StatusCode status code of a file in the Worktree
type StatusCode int8

const (
	Unmodified StatusCode = iota
	Untracked
	Modified
	Added
	Deleted
	Renamed
	Copied
	UpdatedButUnmerged
)

func (c StatusCode) String() string {
	switch c {
	case Unmodified:
		return " "
	case Modified:
		return "M"
	case Added:
		return "A"
	case Deleted:
		return "D"
	case Renamed:
		return "R"
	case Copied:
		return "C"
	case UpdatedButUnmerged:
		return "U"
	case Untracked:
		return "?"
	default:
		return "-"
	}
}

func calcSHA1(fs billy.Filesystem, filename string) (plumbing.Hash, error) {
	file, err := fs.Open(filename)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	stat, err := fs.Stat(filename)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	h := plumbing.NewHasher(plumbing.BlobObject, stat.Size())
	if _, err := io.Copy(h, file); err != nil {
		return plumbing.ZeroHash, err
	}

	return h.Sum(), nil
}

func readDirAll(filesystem billy.Filesystem) (map[string]billy.FileInfo, error) {
	all := make(map[string]billy.FileInfo, 0)
	return all, doReadDirAll(filesystem, "", all)
}

func doReadDirAll(fs billy.Filesystem, path string, files map[string]billy.FileInfo) error {
	if path == ".git" {
		return nil
	}

	l, err := fs.ReadDir(path)
	if err != nil {
		return err
	}

	for _, info := range l {
		file := fs.Join(path, info.Name())
		if !info.IsDir() {
			files[file] = info
			continue
		}

		if err := doReadDirAll(fs, file, files); err != nil {
			return err
		}
	}

	return nil
}
