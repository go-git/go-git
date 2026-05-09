package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5"
)

// worktreeFilesystem wraps a billy.Filesystem and validates every path passed
// to a mutating operation. This prevents writing to, or deleting from,
// dangerous locations (e.g. .git/*, ../) regardless of which worktree
// code path triggers the operation.
type worktreeFilesystem struct {
	billy.Filesystem
	protectNTFS bool
	protectHFS  bool
}

func newWorktreeFilesystem(fs billy.Filesystem, protectNTFS, protectHFS bool) *worktreeFilesystem {
	return &worktreeFilesystem{Filesystem: fs, protectNTFS: protectNTFS, protectHFS: protectHFS}
}

func (sfs *worktreeFilesystem) Create(filename string) (billy.File, error) {
	if err := sfs.validPath(filename); err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	return sfs.Filesystem.Create(filename)
}

func (sfs *worktreeFilesystem) Open(filename string) (billy.File, error) {
	if err := sfs.validReadPath(filename); err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return sfs.Filesystem.Open(filename)
}

func (sfs *worktreeFilesystem) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	if err := sfs.validPath(filename); err != nil {
		return nil, fmt.Errorf("openfile: %w", err)
	}
	return sfs.Filesystem.OpenFile(filename, flag, perm)
}

func (sfs *worktreeFilesystem) Stat(filename string) (os.FileInfo, error) {
	if err := sfs.validReadPath(filename); err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	return sfs.Filesystem.Stat(filename)
}

func (sfs *worktreeFilesystem) Remove(filename string) error {
	if err := sfs.validPath(filename); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	return sfs.Filesystem.Remove(filename)
}

func (sfs *worktreeFilesystem) Rename(from, to string) error {
	if err := sfs.validPath(from, to); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return sfs.Filesystem.Rename(from, to)
}

func (sfs *worktreeFilesystem) ReadDir(path string) ([]os.FileInfo, error) {
	if err := sfs.validReadPath(path); err != nil {
		return nil, fmt.Errorf("readdir: %w", err)
	}
	return sfs.Filesystem.ReadDir(path)
}

func (sfs *worktreeFilesystem) Lstat(filename string) (os.FileInfo, error) {
	if err := sfs.validReadPath(filename); err != nil {
		return nil, fmt.Errorf("lstat: %w", err)
	}
	return sfs.Filesystem.Lstat(filename)
}

func (sfs *worktreeFilesystem) Symlink(target, link string) error {
	if err := sfs.validPath(link); err != nil {
		return fmt.Errorf("symlink: %w", err)
	}
	return sfs.Filesystem.Symlink(target, link)
}

func (sfs *worktreeFilesystem) Readlink(link string) (string, error) {
	if err := sfs.validReadPath(link); err != nil {
		return "", fmt.Errorf("readlink: %w", err)
	}
	return sfs.Filesystem.Readlink(link)
}

func (sfs *worktreeFilesystem) MkdirAll(path string, perm os.FileMode) error {
	if err := sfs.validPath(path); err != nil {
		return fmt.Errorf("mkdirall: %w", err)
	}
	return sfs.Filesystem.MkdirAll(path, perm)
}

func (sfs *worktreeFilesystem) TempFile(dir, prefix string) (billy.File, error) {
	return nil, fmt.Errorf("tempfile: %w", errUnsupportedOperation)
}

func (sfs *worktreeFilesystem) Chroot(path string) (billy.Filesystem, error) {
	if err := sfs.validReadPath(path); err != nil {
		return nil, fmt.Errorf("chroot: %w", err)
	}
	return sfs.Filesystem.Chroot(path)
}

// validReadPath is like validPath but treats the empty string and "." as
// valid references to the worktree root. Read-side operations on the root
// (e.g. ReadDir(""), Lstat(".")) are legitimate; mutating the root itself
// is not, so write-side operations continue to use validPath directly.
func (sfs *worktreeFilesystem) validReadPath(p string) error {
	if p == "" || p == "." || p == "/" {
		return nil
	}
	return sfs.validPath(p)
}

var errUnsupportedOperation = errors.New("unsupported operation")

// worktreeDeny is a list of paths that are not allowed
// to be used when resetting the worktree.
var worktreeDeny = map[string]struct{}{
	// .git
	GitDirName: {},

	// For other historical reasons, file names that do not conform to the 8.3
	// format (up to eight characters for the basename, three for the file
	// extension, certain characters not allowed such as `+`, etc) are associated
	// with a so-called "short name", at least on the `C:` drive by default.
	// Which means that `git~1/` is a valid way to refer to `.git/`.
	"git~1": {},
}

// validPath checks whether paths are valid.
// The rules around invalid paths could differ from upstream based on how
// filesystems are managed within go-git, but they are largely the same.
//
// For upstream rules:
// https://github.com/git/git/blob/564d0252ca632e0264ed670534a51d18a689ef5d/read-cache.c#L946
// https://github.com/git/git/blob/564d0252ca632e0264ed670534a51d18a689ef5d/path.c#L1383
func (sfs *worktreeFilesystem) validPath(paths ...string) error {
	for _, p := range paths {
		for _, r := range p {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("invalid path %q: contains control character", p)
			}
		}

		parts := strings.FieldsFunc(p, func(r rune) bool { return (r == '\\' || r == '/') })
		if len(parts) == 0 {
			return fmt.Errorf("invalid path: %q", p)
		}

		if sfs.protectNTFS {
			// Volume names are not supported, in both formats: \\ and <DRIVE_LETTER>:.
			if vol := filepath.VolumeName(p); vol != "" {
				return fmt.Errorf("invalid path: %q", p)
			}
		}

		for i, part := range parts {
			if part == "." || part == ".." {
				return fmt.Errorf("invalid path %q: cannot use %q", p, part)
			}

			// Reject .git (and equivalents) as a path component when it is
			// either the first component (root-level .git) or a non-final
			// component (traversal into a .git directory, e.g. "a/.git/config").
			// A final non-first .git component (e.g. "submodule/.git") is
			// allowed because submodule worktrees contain a .git pointer file.
			isDotGit := false
			if _, denied := worktreeDeny[strings.ToLower(part)]; denied {
				isDotGit = true
			} else if sfs.protectHFS && isHFSDotGit(part) {
				isDotGit = true
			}

			if isDotGit && (i == 0 || i < len(parts)-1) {
				return fmt.Errorf("invalid path component: %q", p)
			}

			if sfs.protectNTFS && !windowsValidPath(part) {
				return fmt.Errorf("invalid path: %q", p)
			}
		}
	}
	return nil
}
