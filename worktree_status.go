package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6/util"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/gitignore"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/convert"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/filesystem"
	mindex "github.com/go-git/go-git/v6/utils/merkletrie/index"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
	"github.com/go-git/go-git/v6/utils/sync"
	"github.com/go-git/go-git/v6/utils/trace"
)

var (
	// ErrDestinationExists in an Move operation means that the target exists on
	// the worktree.
	ErrDestinationExists = errors.New("destination exists")
	// ErrGlobNoMatches in an AddGlob if the glob pattern does not match any
	// files in the worktree.
	ErrGlobNoMatches = errors.New("glob pattern did not match any files")
	// ErrUnsupportedStatusStrategy occurs when an invalid StatusStrategy is used
	// when processing the Worktree status.
	ErrUnsupportedStatusStrategy = errors.New("unsupported status strategy")
)

// Status returns the working tree status.
func (w *Worktree) Status(ctx context.Context) (Status, error) {
	return w.StatusWithOptions(ctx, StatusOptions{Strategy: defaultStatusStrategy})
}

// StatusOptions defines the options for Worktree.StatusWithOptions().
type StatusOptions struct {
	Strategy StatusStrategy
}

// StatusWithOptions returns the working tree status.
func (w *Worktree) StatusWithOptions(ctx context.Context, o StatusOptions) (Status, error) {
	var hash plumbing.Hash

	ref, err := w.r.Head(ctx)
	if err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil, err
	}

	if err == nil {
		hash = ref.Hash()
	}

	cfg, err := w.r.Config(ctx)
	if err != nil {
		return nil, err
	}

	return w.status(ctx, cfg, o.Strategy, hash)
}

func (w *Worktree) status(ctx context.Context, cfg *config.Config, ss StatusStrategy, commit plumbing.Hash) (Status, error) {
	s, err := ss.new(ctx, w)
	if err != nil {
		return nil, err
	}

	left, err := w.diffCommitWithStaging(ctx, commit, false)
	if err != nil {
		return nil, err
	}

	for _, ch := range left {
		a, err := ch.Action()
		if err != nil {
			return nil, err
		}

		fs := s.File(nameFromAction(&ch))
		fs.Worktree = Unmodified

		switch a {
		case merkletrie.Delete:
			s.File(ch.From.String()).Staging = Deleted
		case merkletrie.Insert:
			s.File(ch.To.String()).Staging = Added
		case merkletrie.Modify:
			s.File(ch.To.String()).Staging = Modified
		}
	}

	right, err := w.diffStagingWithWorktree(ctx, cfg, false, true)
	if err != nil {
		return nil, err
	}

	for _, ch := range right {
		a, err := ch.Action()
		if err != nil {
			return nil, err
		}

		fs := s.File(nameFromAction(&ch))
		if fs.Staging == Untracked {
			fs.Staging = Unmodified
		}

		switch a {
		case merkletrie.Delete:
			fs.Worktree = Deleted
		case merkletrie.Insert:
			fs.Worktree = Untracked
			fs.Staging = Untracked
		case merkletrie.Modify:
			fs.Worktree = Modified
		}
	}

	return s, nil
}

func nameFromAction(ch *merkletrie.Change) string {
	name := ch.To.String()
	if name == "" {
		return ch.From.String()
	}

	return name
}

func (w *Worktree) diffStagingWithWorktree(ctx context.Context, cfg *config.Config, reverse, excludeIgnoredChanges bool) (merkletrie.Changes, error) {
	idx, err := w.r.Storer.Index(ctx)
	if err != nil {
		return nil, err
	}

	from := mindex.NewRootNodeWithOptions(idx, mindex.RootNodeOptions{
		UpholdExecutableBit: cfg.Core.FileMode,
	})
	submodules, err := w.getSubmodulesStatus(ctx, cfg)
	if err != nil {
		return nil, err
	}

	fsOpts := filesystem.Options{
		AutoCRLF: cfg.Core.AutoCRLF == "true" || cfg.Core.AutoCRLF == "input",
		Index:    idx,
	}

	// When ignored changes are to be filtered out, gather the gitignore
	// patterns once, build a matcher, and let the filesystem noder skip
	// ignored, untracked entries during the walk. This avoids descending
	// into large gitignored directories like node_modules and makes a
	// post-walk filter unnecessary: tracked entries are still walked even
	// when their parent matches an ignore rule, so modifications to them
	// are still reported.
	if excludeIgnoredChanges {
		if patterns := w.collectIgnorePatterns(); len(patterns) > 0 {
			fsOpts.IgnoreMatcher = gitignore.NewMatcher(patterns)
		}
	}

	to := filesystem.NewRootNodeWithOptions(w.filesystem, submodules, fsOpts)

	if reverse {
		return merkletrie.DiffTreeContext(ctx, to, from, diffTreeIsEquals)
	}
	return merkletrie.DiffTreeContext(ctx, from, to, diffTreeIsEquals)
}

func (w *Worktree) collectIgnorePatterns() []gitignore.Pattern {
	patterns, err := gitignore.ReadPatterns(w.filesystem, nil)
	if err != nil {
		patterns = nil
	}
	return append(patterns, w.Excludes...)
}

func (w *Worktree) getSubmodulesStatus(ctx context.Context, cfg *config.Config) (map[string]plumbing.Hash, error) {
	o := map[string]plumbing.Hash{}

	sub, err := w.submodulesWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	status, err := sub.Status(ctx)
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

func (w *Worktree) diffCommitWithStaging(ctx context.Context, commit plumbing.Hash, reverse bool) (merkletrie.Changes, error) {
	var t *object.Tree
	if !commit.IsZero() {
		c, err := w.r.CommitObject(ctx, commit)
		if err != nil {
			return nil, err
		}

		t, err = c.Tree(ctx)
		if err != nil {
			return nil, err
		}
	}

	return w.diffTreeWithStaging(ctx, t, reverse)
}

func (w *Worktree) diffTreeWithStaging(ctx context.Context, t *object.Tree, reverse bool) (merkletrie.Changes, error) {
	var from noder.Noder
	if t != nil {
		from = object.NewTreeRootNode(t)
	}

	idx, err := w.r.Storer.Index(ctx)
	if err != nil {
		return nil, err
	}

	to := mindex.NewRootNode(idx)

	if reverse {
		return merkletrie.DiffTreeContext(ctx, to, from, diffTreeIsEquals)
	}

	return merkletrie.DiffTreeContext(ctx, from, to, diffTreeIsEquals)
}

// diffTrees returns the changes between two tree objects.
// Either tree may be nil, which is treated as the empty tree.
func diffTrees(ctx context.Context, from, to *object.Tree) (merkletrie.Changes, error) {
	var fromNode, toNode noder.Noder
	if from != nil {
		fromNode = object.NewTreeRootNode(from)
	}
	if to != nil {
		toNode = object.NewTreeRootNode(to)
	}
	return merkletrie.DiffTreeContext(ctx, fromNode, toNode, diffTreeIsEquals)
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
// file is already staged in the index no error is returned. If a file deleted
// from the Workspace is given, the file is removed from the index. If a
// directory given, adds the files and all his sub-directories recursively in
// the worktree to the index. If any of the files is already staged in the index
// no error is returned. When path is a file, the blob.Hash is returned.
func (w *Worktree) Add(ctx context.Context, path string) (plumbing.Hash, error) {
	// TODO(mcuadros): deprecate in favor of AddWithOption in v6.
	return w.doAdd(ctx, path, make([]gitignore.Pattern, 0), false)
}

func (w *Worktree) doAddDirectory(ctx context.Context, cfg *config.Config, idx *index.Index, s Status, directory string, ignorePattern []gitignore.Pattern) (added bool, err error) {
	if len(ignorePattern) > 0 {
		m := gitignore.NewMatcher(ignorePattern)
		matchPath := strings.Split(directory, string(os.PathSeparator))
		if m.Match(matchPath, true) {
			// ignore
			return false, nil
		}
	}

	directory = filepath.ToSlash(filepath.Clean(directory))

	for name := range s {
		if !isPathInDirectory(name, directory) {
			continue
		}

		var a bool
		a, _, err = w.doAddFile(ctx, cfg, idx, s, name, ignorePattern)
		if err != nil {
			return added, err
		}

		added = added || a
	}

	return added, err
}

func isPathInDirectory(path, directory string) bool {
	return directory == "." || strings.HasPrefix(path, directory+"/")
}

// AddWithOptions file contents to the index,  updates the index using the
// current content found in the working tree, to prepare the content staged for
// the next commit.
//
// It typically adds the current content of existing paths as a whole, but with
// some options it can also be used to add content with only part of the changes
// made to the working tree files applied, or remove paths that do not exist in
// the working tree anymore.
func (w *Worktree) AddWithOptions(ctx context.Context, opts *AddOptions) error {
	if err := opts.Validate(w.r); err != nil {
		return err
	}

	if opts.All {
		_, err := w.doAdd(ctx, ".", w.Excludes, false)
		return err
	}

	if opts.Glob != "" {
		return w.AddGlob(ctx, opts.Glob)
	}

	_, err := w.doAdd(ctx, opts.Path, make([]gitignore.Pattern, 0), opts.SkipStatus)
	return err
}

func (w *Worktree) doAdd(ctx context.Context, path string, ignorePattern []gitignore.Pattern, skipStatus bool) (plumbing.Hash, error) {
	if trace.Performance.Enabled() {
		start := time.Now()
		defer func() {
			trace.Performance.Printf("performance: %.9f s: git command: git add %s", time.Since(start).Seconds(), path)
		}()
	}

	cfg, err := w.r.Config(ctx)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	idx, err := w.r.Storer.Index(ctx)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	var h plumbing.Hash
	var added bool

	fi, err := w.filesystem.Lstat(path)

	// status is required for doAddDirectory
	var s Status
	var err2 error
	if !skipStatus || fi == nil || fi.IsDir() {
		s, err2 = w.Status(ctx)
		if err2 != nil {
			return plumbing.ZeroHash, err2
		}
	}

	path = filepath.Clean(path)
	if filepath.IsAbs(path) {
		root := w.filesystem.Root()
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("path %q is not inside the worktree root %q: %w", path, root, err)
		}
		if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			return plumbing.ZeroHash, fmt.Errorf("path %q is outside the worktree root %q", path, root)
		}
		path = relPath
	}
	path = filepath.ToSlash(path)

	if err != nil || !fi.IsDir() {
		added, h, err = w.doAddFile(ctx, cfg, idx, s, path, ignorePattern)
	} else {
		added, err = w.doAddDirectory(ctx, cfg, idx, s, path, ignorePattern)
	}

	if err != nil {
		return h, err
	}

	if !added {
		return h, nil
	}

	return h, w.r.Storer.SetIndex(ctx, idx)
}

// AddGlob adds all paths, matching pattern, to the index. If pattern matches a
// directory path, all directory contents are added to the index recursively. No
// error is returned if all matching paths are already staged in index.
func (w *Worktree) AddGlob(ctx context.Context, pattern string) error {
	if trace.Performance.Enabled() {
		start := time.Now()
		defer func() {
			trace.Performance.Printf("performance: %.9f s: add glob %s", time.Since(start).Seconds(), pattern)
		}()
	}

	// TODO(mcuadros): deprecate in favor of AddWithOption in v6.
	files, err := util.Glob(w.filesystem, pattern)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		return ErrGlobNoMatches
	}

	cfg, err := w.r.Config(ctx)
	if err != nil {
		return err
	}

	s, err := w.Status(ctx)
	if err != nil {
		return err
	}

	idx, err := w.r.Storer.Index(ctx)
	if err != nil {
		return err
	}

	var saveIndex bool
	for _, file := range files {
		fi, err := w.filesystem.Lstat(file)
		if err != nil {
			return err
		}

		var added bool
		if fi.IsDir() {
			added, err = w.doAddDirectory(ctx, cfg, idx, s, file, make([]gitignore.Pattern, 0))
		} else {
			added, _, err = w.doAddFile(ctx, cfg, idx, s, file, make([]gitignore.Pattern, 0))
		}

		if err != nil {
			return err
		}

		if !saveIndex && added {
			saveIndex = true
		}
	}

	if saveIndex {
		return w.r.Storer.SetIndex(ctx, idx)
	}

	return nil
}

// doAddFile create a new blob from path and update the index, added is true if
// the file added is different from the index.
// if s status is nil will skip the status check and update the index anyway
func (w *Worktree) doAddFile(ctx context.Context, cfg *config.Config, idx *index.Index, s Status, path string, ignorePattern []gitignore.Pattern) (added bool, h plumbing.Hash, err error) {
	if s != nil && s.File(path).Worktree == Unmodified {
		return false, h, nil
	}
	if len(ignorePattern) > 0 {
		m := gitignore.NewMatcher(ignorePattern)
		matchPath := strings.Split(path, string(os.PathSeparator))
		if m.Match(matchPath, true) {
			// ignore
			return false, h, nil
		}
	}

	h, err = w.copyFileToStorage(ctx, cfg, path)
	if err != nil {
		if os.IsNotExist(err) {
			added = true
			h, err = w.deleteFromIndex(idx, path)
		}

		return added, h, err
	}

	if err := w.addOrUpdateFileToIndex(idx, path, h); err != nil {
		return false, h, err
	}

	return true, h, err
}

func (w *Worktree) copyFileToStorage(ctx context.Context, cfg *config.Config, path string) (hash plumbing.Hash, err error) {
	fi, err := w.filesystem.Lstat(path)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	obj := w.r.Storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	obj.SetSize(fi.Size())

	writer, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	defer ioutil.CheckClose(writer, &err)

	if fi.Mode()&os.ModeSymlink != 0 {
		err = w.fillEncodedObjectFromSymlink(writer, path, fi)
	} else {
		err = w.fillEncodedObjectFromFile(cfg, writer, path, fi)
	}

	if err != nil {
		return plumbing.ZeroHash, err
	}

	return w.r.Storer.SetEncodedObject(ctx, obj)
}

func (w *Worktree) fillEncodedObjectFromFile(cfg *config.Config, dst io.Writer, path string, _ os.FileInfo) (err error) {
	file, err := w.filesystem.Open(path)
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(file, &err)

	switch cfg.Core.AutoCRLF {
	case "true", "input":
		br := sync.GetBufioReader(file)
		defer sync.PutBufioReader(br)

		stat, err := convert.GetStat(br)
		if err != nil {
			return err
		}

		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return err
		}

		if !stat.IsBinary() {
			dst = convert.NewLFWriter(dst)
		}
	}

	_, err = ioutil.CopyBufferPool(dst, file)
	return err
}

func (w *Worktree) fillEncodedObjectFromSymlink(dst io.Writer, path string, _ os.FileInfo) error {
	target, err := w.filesystem.Readlink(path)
	if err != nil {
		return err
	}

	_, err = dst.Write([]byte(target))
	return err
}

func (w *Worktree) addOrUpdateFileToIndex(idx *index.Index, filename string, h plumbing.Hash) error {
	e, err := idx.Entry(filename)
	if err != nil && !errors.Is(err, index.ErrEntryNotFound) {
		return err
	}

	if errors.Is(err, index.ErrEntryNotFound) {
		return w.doAddFileToIndex(idx, filename, h)
	}

	return w.doUpdateFileToIndex(e, filename, h)
}

func (w *Worktree) doAddFileToIndex(idx *index.Index, filename string, h plumbing.Hash) error {
	e, err := idx.Add(filename)
	if err != nil {
		return err
	}
	return w.doUpdateFileToIndex(e, filename, h)
}

func (w *Worktree) doUpdateFileToIndex(e *index.Entry, filename string, h plumbing.Hash) error {
	info, err := w.filesystem.Lstat(filename)
	if err != nil {
		return err
	}

	e.Hash = h
	e.ModifiedAt = info.ModTime()
	e.Mode, err = filemode.NewFromOSFileMode(info.Mode())
	if err != nil {
		return err
	}

	// The entry size must always reflect the current state, otherwise
	// it will cause go-git's Worktree.Status() to divert from "git status".
	// The size of a symlink is the length of the path to the target.
	// The size of Regular and Executable files is the size of the files.
	e.Size = uint32(info.Size())

	fillSystemInfo(e, info.Sys())
	return nil
}

// Remove removes files from the working tree and from the index.
func (w *Worktree) Remove(ctx context.Context, path string) (plumbing.Hash, error) {
	// TODO(mcuadros): remove plumbing.Hash from signature at v5.
	idx, err := w.r.Storer.Index(ctx)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	var h plumbing.Hash

	fi, err := w.filesystem.Lstat(path)
	if err != nil || !fi.IsDir() {
		h, err = w.doRemoveFile(idx, path)
	} else {
		_, err = w.doRemoveDirectory(idx, path)
	}
	if err != nil {
		return h, err
	}

	return h, w.r.Storer.SetIndex(ctx, idx)
}

func (w *Worktree) doRemoveDirectory(idx *index.Index, directory string) (removed bool, err error) {
	files, err := w.filesystem.ReadDir(directory)
	if err != nil {
		return false, err
	}

	for _, file := range files {
		name := path.Join(directory, file.Name())

		var r bool
		if file.IsDir() {
			r, err = w.doRemoveDirectory(idx, name)
		} else {
			_, err = w.doRemoveFile(idx, name)
			if errors.Is(err, index.ErrEntryNotFound) {
				err = nil
			}
		}

		if err != nil {
			return removed, err
		}

		if !removed && r {
			removed = true
		}
	}

	err = w.removeEmptyDirectory(directory)
	return removed, err
}

func (w *Worktree) removeEmptyDirectory(path string) error {
	files, err := w.filesystem.ReadDir(path)
	if err != nil {
		return err
	}

	if len(files) != 0 {
		return nil
	}

	return w.filesystem.Remove(path)
}

func (w *Worktree) doRemoveFile(idx *index.Index, path string) (plumbing.Hash, error) {
	hash, err := w.deleteFromIndex(idx, path)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return hash, w.deleteFromFilesystem(path)
}

func (w *Worktree) deleteFromIndex(idx *index.Index, path string) (plumbing.Hash, error) {
	e, err := idx.Remove(path)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return e.Hash, nil
}

func (w *Worktree) deleteFromFilesystem(path string) error {
	err := w.filesystem.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}

	return err
}

// RemoveGlob removes all paths, matching pattern, from the index. If pattern
// matches a directory path, all directory contents are removed from the index
// recursively.
func (w *Worktree) RemoveGlob(ctx context.Context, pattern string) error {
	idx, err := w.r.Storer.Index(ctx)
	if err != nil {
		return err
	}

	entries, err := idx.Glob(pattern)
	if err != nil {
		return err
	}

	for _, e := range entries {
		file := filepath.FromSlash(e.Name)
		if _, err := w.filesystem.Lstat(file); err != nil && !os.IsNotExist(err) {
			return err
		}

		if _, err := w.doRemoveFile(idx, file); err != nil {
			return err
		}

		dir, _ := filepath.Split(file)
		if err := w.removeEmptyDirectory(dir); err != nil {
			return err
		}
	}

	return w.r.Storer.SetIndex(ctx, idx)
}

// Move moves or rename a file in the worktree and the index, directories are
// not supported.
func (w *Worktree) Move(ctx context.Context, from, to string) (plumbing.Hash, error) {
	// TODO(mcuadros): support directories and/or implement support for glob
	if _, err := w.filesystem.Lstat(from); err != nil {
		return plumbing.ZeroHash, err
	}

	if _, err := w.filesystem.Lstat(to); err == nil {
		return plumbing.ZeroHash, ErrDestinationExists
	}

	idx, err := w.r.Storer.Index(ctx)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	hash, err := w.deleteFromIndex(idx, from)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	if err := w.filesystem.Rename(from, to); err != nil {
		return hash, err
	}

	if err := w.addOrUpdateFileToIndex(idx, to, hash); err != nil {
		return hash, err
	}

	return hash, w.r.Storer.SetIndex(ctx, idx)
}
