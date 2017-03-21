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

	"gopkg.in/src-d/go-billy.v2"
)

var ErrWorktreeNotClean = errors.New("worktree is not clean")
var ErrSubmoduleNotFound = errors.New("submodule not found")

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

	c, err := w.r.CommitObject(commit)
	if err != nil {
		return err
	}

	t, err := c.Tree()
	if err != nil {
		return err
	}

	idx := &index.Index{Version: 2}
	walker := object.NewTreeWalker(t, true)

	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		if err := w.checkoutEntry(name, &entry, idx); err != nil {
			return err
		}
	}

	return w.r.Storer.SetIndex(idx)
}

func (w *Worktree) checkoutEntry(name string, e *object.TreeEntry, idx *index.Index) error {
	if e.Mode == filemode.Submodule {
		return w.addIndexFromTreeEntry(name, e, idx)
	}

	if e.Mode == filemode.Dir {
		return nil
	}

	return w.checkoutFile(name, e, idx)
}

func (w *Worktree) checkoutFile(name string, e *object.TreeEntry, idx *index.Index) error {
	blob, err := object.GetBlob(w.r.Storer, e.Hash)
	if err != nil {
		return err
	}

	from, err := blob.Reader()
	if err != nil {
		return err
	}
	defer from.Close()

	mode, err := e.Mode.ToOSFileMode()
	if err != nil {
		return err
	}

	to, err := w.fs.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer to.Close()

	if _, err := io.Copy(to, from); err != nil {
		return err
	}

	return w.addIndexFromFile(name, e, idx)
}

var fillSystemInfo func(e *index.Entry, sys interface{})

func (w *Worktree) addIndexFromTreeEntry(name string, f *object.TreeEntry, idx *index.Index) error {
	idx.Entries = append(idx.Entries, index.Entry{
		Hash: f.Hash,
		Name: name,
		Mode: filemode.Submodule,
	})

	return nil
}

func (w *Worktree) addIndexFromFile(name string, f *object.TreeEntry, idx *index.Index) error {
	fi, err := w.fs.Stat(name)
	if err != nil {
		return err
	}

	mode, err := filemode.NewFromOSFileMode(fi.Mode())
	if err != nil {
		return err
	}

	e := index.Entry{
		Hash:       f.Hash,
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

func (w *Worktree) Status() (Status, error) {
	idx, err := w.r.Storer.Index()
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

	mode, err := filemode.NewFromOSFileMode(fi.Mode())
	if err != nil {
		return Modified, err
	}

	if mode != e.Mode {
		return Modified, nil
	}

	h, err := calcSHA1(w.fs, e.Name)
	if h != e.Hash || err != nil {
		return Modified, err

	}

	return Unmodified, nil
}

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
	if path == defaultDotGitPath {
		return nil
	}

	l, err := fs.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	for _, info := range l {
		file := fs.Join(path, info.Name())
		if file == defaultDotGitPath {
			continue
		}

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
