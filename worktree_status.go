package git

import (
	"bytes"
	"io"

	"fmt"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/filemode"
	"gopkg.in/src-d/go-git.v4/plumbing/format/index"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/utils/ioutil"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie/filesystem"
	mindex "gopkg.in/src-d/go-git.v4/utils/merkletrie/index"
	"gopkg.in/src-d/go-git.v4/utils/merkletrie/noder"
)

// Status returns the working tree status.
func (w *Worktree) Status() (Status, error) {
	ref, err := w.r.Head()
	if err == plumbing.ErrReferenceNotFound {
		return make(Status, 0), nil
	}

	if err != nil {
		return nil, err
	}

	return w.status(ref.Hash())
}

func (w *Worktree) status(commit plumbing.Hash) (Status, error) {
	s := make(Status, 0)

	left, err := w.diffCommitWithStaging(commit, false)
	if err != nil {
		return nil, err
	}

	for _, ch := range left {
		a, err := ch.Action()
		if err != nil {
			return nil, err
		}

		switch a {
		case merkletrie.Delete:
			s.File(ch.From.String()).Staging = Deleted
		case merkletrie.Insert:
			s.File(ch.To.String()).Staging = Added
		case merkletrie.Modify:
			s.File(ch.To.String()).Staging = Modified
		}
	}

	right, err := w.diffStagingWithWorktree()
	if err != nil {
		return nil, err
	}

	for _, ch := range right {
		a, err := ch.Action()
		if err != nil {
			return nil, err
		}

		switch a {
		case merkletrie.Delete:
			s.File(ch.From.String()).Worktree = Deleted
		case merkletrie.Insert:
			s.File(ch.To.String()).Worktree = Untracked
			s.File(ch.To.String()).Staging = Untracked
		case merkletrie.Modify:
			s.File(ch.To.String()).Worktree = Modified
		}
	}

	return s, nil
}

func (w *Worktree) diffStagingWithWorktree() (merkletrie.Changes, error) {
	idx, err := w.r.Storer.Index()
	if err != nil {
		return nil, err
	}

	from := mindex.NewRootNode(idx)
	submodules, err := w.getSubmodulesStatus()
	if err != nil {
		return nil, err
	}

	to := filesystem.NewRootNode(w.fs, submodules)
	return merkletrie.DiffTree(from, to, diffTreeIsEquals)
}

func (w *Worktree) getSubmodulesStatus() (map[string]plumbing.Hash, error) {
	o := map[string]plumbing.Hash{}

	sub, err := w.Submodules()
	if err != nil {
		return nil, err
	}

	status, err := sub.Status()
	if err != nil {
		return nil, err
	}

	for _, s := range status {
		if s.Current.IsZero() {
			o[s.Path] = s.Expected
			continue
		}

		o[s.Path] = s.Current
	}

	return o, nil
}

func (w *Worktree) diffCommitWithStaging(commit plumbing.Hash, reverse bool) (merkletrie.Changes, error) {
	idx, err := w.r.Storer.Index()
	if err != nil {
		return nil, err
	}

	c, err := w.r.CommitObject(commit)
	if err != nil {
		return nil, err
	}

	t, err := c.Tree()
	if err != nil {
		return nil, err
	}

	to := mindex.NewRootNode(idx)
	from := object.NewTreeRootNode(t)

	if reverse {
		return merkletrie.DiffTree(to, from, diffTreeIsEquals)
	}

	return merkletrie.DiffTree(from, to, diffTreeIsEquals)
}

var emptyNoderHash = make([]byte, 24)

// diffTreeIsEquals is a implementation of noder.Equals, used to compare
// noder.Noder, it compare the content and the length of the hashes.
//
// Since some of the noder.Noder implementations doesn't compute a hash for
// some directories, if any of the hashes is a 24-byte slice of zero values
// the comparison is not done and the hashes are take as different.
func diffTreeIsEquals(a, b noder.Hasher) bool {
	hashA := a.Hash()
	hashB := b.Hash()

	if bytes.Equal(hashA, emptyNoderHash) || bytes.Equal(hashB, emptyNoderHash) {
		return false
	}

	return bytes.Equal(hashA, hashB)
}

// Add adds the file contents of a file in the worktree to the index. if the
// file is already stagged in the index no error is returned.
func (w *Worktree) Add(path string) (plumbing.Hash, error) {
	s, err := w.Status()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	h, err := w.calculateBlobHash(path)
	if err != nil {
		return h, err
	}

	fmt.Println(len(s))
	fs := s.File(path)
	if fs != nil && fs.Worktree == Unmodified {
		return h, nil
	}

	if err := w.addOrUpdateFileToIndex(path, h); err != nil {
		return h, err
	}

	return h, err
}

func (w *Worktree) calculateBlobHash(filename string) (hash plumbing.Hash, err error) {
	fi, err := w.fs.Stat(filename)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	f, err := w.fs.Open(filename)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	defer ioutil.CheckClose(f, &err)

	h := plumbing.NewHasher(plumbing.BlobObject, fi.Size())
	if _, err := io.Copy(h, f); err != nil {
		return plumbing.ZeroHash, err
	}

	hash = h.Sum()
	return
}

func (w *Worktree) addOrUpdateFileToIndex(filename string, h plumbing.Hash) error {
	idx, err := w.r.Storer.Index()
	if err != nil {
		return err
	}

	_, err = idx.Entry(filename)
	if err == index.ErrEntryNotFound {
		err = w.doAddFileToIndex(idx, filename)
	}

	if err != nil {
		return err
	}

	err = w.doUpdateFileToIndex(idx, filename, h)
	if err != nil {
		return err
	}

	return w.r.Storer.SetIndex(idx)
}

func (w *Worktree) doAddFileToIndex(idx *index.Index, filename string) error {
	idx.Entries = append(idx.Entries, &index.Entry{
		Name: filename,
	})

	return nil
}

func (w *Worktree) doUpdateFileToIndex(idx *index.Index, filename string, h plumbing.Hash) error {
	info, err := w.fs.Stat(filename)
	if err != nil {
		return err
	}

	e, err := idx.Entry(filename)
	if err != nil {
		return err
	}

	e.Hash = h
	e.ModifiedAt = info.ModTime()
	e.Mode, err = filemode.NewFromOSFileMode(info.Mode())
	if err != nil {
		return err
	}

	fillSystemInfo(e, info.Sys())
	return nil
}
