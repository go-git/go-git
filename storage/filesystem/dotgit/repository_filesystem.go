package dotgit

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v6"
)

// RepositoryFilesystem is a billy.Filesystem compatible object wrapper
// which handles dot-git filesystem operations and supports commondir according to git scm layout:
// https://github.com/git/git/blob/master/Documentation/gitrepository-layout.adoc
type RepositoryFilesystem struct {
	dotGitFs       billy.Filesystem
	commonDotGitFs billy.Filesystem
}

// NewRepositoryFilesystem creates a new RepositoryFilesystem.
func NewRepositoryFilesystem(dotGitFs, commonDotGitFs billy.Filesystem) *RepositoryFilesystem {
	return &RepositoryFilesystem{
		dotGitFs:       dotGitFs,
		commonDotGitFs: commonDotGitFs,
	}
}

func (fs *RepositoryFilesystem) mapToRepositoryFsByPath(path string) billy.Filesystem {
	// Nothing to decide if commondir not defined
	if fs.commonDotGitFs == nil {
		return fs.dotGitFs
	}

	cleanPath := filepath.Clean(path)

	// Handle absolute paths by checking if they're under commonDotGitFs or dotGitFs.
	// This is needed because temp files return absolute paths from Name(), and operations
	// like Rename need to route to the correct filesystem.
	if filepath.IsAbs(cleanPath) {
		commonRoot := fs.commonDotGitFs.Root()
		dotGitRoot := fs.dotGitFs.Root()

		if strings.HasPrefix(cleanPath, commonRoot+string(filepath.Separator)) || cleanPath == commonRoot {
			return fs.commonDotGitFs
		}
		if strings.HasPrefix(cleanPath, dotGitRoot+string(filepath.Separator)) || cleanPath == dotGitRoot {
			return fs.dotGitFs
		}
		// Absolute path doesn't match either root - default to dotGitFs.
		// This shouldn't occur in normal usage.
		return fs.dotGitFs
	}

	// Check exceptions for commondir (https://git-scm.com/docs/gitrepository-layout#Documentation/gitrepository-layout.txt)
	switch cleanPath {
	case fs.dotGitFs.Join(logsPath, "HEAD"):
		return fs.dotGitFs
	case fs.dotGitFs.Join(refsPath, "bisect"), fs.dotGitFs.Join(refsPath, "rewritten"), fs.dotGitFs.Join(refsPath, "worktree"):
		return fs.dotGitFs
	}

	// Determine dot-git root by first path element.
	// There are some elements which should always use commondir when commondir defined.
	// Usual dot-git root will be used for the rest of files.
	switch strings.Split(cleanPath, string(filepath.Separator))[0] {
	case objectsPath, refsPath, packedRefsPath, configPath, branchesPath, hooksPath, infoPath, remotesPath, logsPath, shallowPath, worktreesPath:
		return fs.commonDotGitFs
	default:
		return fs.dotGitFs
	}
}

// Create creates a file in the appropriate filesystem.
func (fs *RepositoryFilesystem) Create(filename string) (billy.File, error) {
	return fs.mapToRepositoryFsByPath(filename).Create(filename)
}

// Open opens a file from the appropriate filesystem.
func (fs *RepositoryFilesystem) Open(filename string) (billy.File, error) {
	return fs.mapToRepositoryFsByPath(filename).Open(filename)
}

// OpenFile opens a file with the given flags and permissions.
func (fs *RepositoryFilesystem) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	return fs.mapToRepositoryFsByPath(filename).OpenFile(filename, flag, perm)
}

// Stat returns file info from the appropriate filesystem.
func (fs *RepositoryFilesystem) Stat(filename string) (os.FileInfo, error) {
	return fs.mapToRepositoryFsByPath(filename).Stat(filename)
}

// Rename renames a file in the appropriate filesystem.
func (fs *RepositoryFilesystem) Rename(oldpath, newpath string) error {
	return fs.mapToRepositoryFsByPath(oldpath).Rename(oldpath, newpath)
}

// Remove removes a file from the appropriate filesystem.
func (fs *RepositoryFilesystem) Remove(filename string) error {
	return fs.mapToRepositoryFsByPath(filename).Remove(filename)
}

// Join joins path elements using the dot-git filesystem.
func (fs *RepositoryFilesystem) Join(elem ...string) string {
	return fs.dotGitFs.Join(elem...)
}

// TempFile creates a temporary file in the appropriate filesystem.
func (fs *RepositoryFilesystem) TempFile(dir, prefix string) (billy.File, error) {
	return fs.mapToRepositoryFsByPath(dir).TempFile(dir, prefix)
}

// ReadDir reads a directory from the appropriate filesystem.
func (fs *RepositoryFilesystem) ReadDir(path string) ([]fs.DirEntry, error) {
	return fs.mapToRepositoryFsByPath(path).ReadDir(path)
}

// MkdirAll creates directories in the appropriate filesystem.
func (fs *RepositoryFilesystem) MkdirAll(filename string, perm os.FileMode) error {
	return fs.mapToRepositoryFsByPath(filename).MkdirAll(filename, perm)
}

// Lstat returns file info without following symlinks.
func (fs *RepositoryFilesystem) Lstat(filename string) (os.FileInfo, error) {
	return fs.mapToRepositoryFsByPath(filename).Lstat(filename)
}

// Symlink creates a symlink in the appropriate filesystem.
func (fs *RepositoryFilesystem) Symlink(target, link string) error {
	return fs.mapToRepositoryFsByPath(target).Symlink(target, link)
}

// Readlink reads the target of a symlink.
func (fs *RepositoryFilesystem) Readlink(link string) (string, error) {
	return fs.mapToRepositoryFsByPath(link).Readlink(link)
}

// Chroot returns a new filesystem rooted at the given path.
func (fs *RepositoryFilesystem) Chroot(path string) (billy.Filesystem, error) {
	return fs.mapToRepositoryFsByPath(path).Chroot(path)
}

// Root returns the root path of the dot-git filesystem.
func (fs *RepositoryFilesystem) Root() string {
	return fs.dotGitFs.Root()
}
