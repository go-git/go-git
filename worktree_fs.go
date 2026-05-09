package git

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
}

func newWorktreeFilesystem(fs billy.Filesystem, protectNTFS bool) *worktreeFilesystem {
	return &worktreeFilesystem{Filesystem: fs, protectNTFS: protectNTFS}
}

func (sfs *worktreeFilesystem) Create(filename string) (billy.File, error) {
	if err := sfs.validPath(filename); err != nil {
		return nil, err
	}
	return sfs.Filesystem.Create(filename)
}

func (sfs *worktreeFilesystem) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	if err := sfs.validPath(filename); err != nil {
		return nil, err
	}
	return sfs.Filesystem.OpenFile(filename, flag, perm)
}

func (sfs *worktreeFilesystem) Remove(filename string) error {
	if err := sfs.validPath(filename); err != nil {
		return err
	}
	return sfs.Filesystem.Remove(filename)
}

func (sfs *worktreeFilesystem) Rename(from, to string) error {
	if err := sfs.validPath(from, to); err != nil {
		return err
	}
	return sfs.Filesystem.Rename(from, to)
}

func (sfs *worktreeFilesystem) Symlink(target, link string) error {
	if err := sfs.validPath(link); err != nil {
		return err
	}
	return sfs.Filesystem.Symlink(target, link)
}

func (sfs *worktreeFilesystem) MkdirAll(path string, perm os.FileMode) error {
	if err := sfs.validPath(path); err != nil {
		return err
	}
	return sfs.Filesystem.MkdirAll(path, perm)
}

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
		parts := strings.FieldsFunc(p, func(r rune) bool { return (r == '\\' || r == '/') })
		if len(parts) == 0 {
			return fmt.Errorf("invalid path: %q", p)
		}

		if _, denied := worktreeDeny[strings.ToLower(parts[0])]; denied {
			return fmt.Errorf("invalid path prefix: %q", p)
		}

		if sfs.protectNTFS {
			// Volume names are not supported, in both formats: \\ and <DRIVE_LETTER>:.
			if vol := filepath.VolumeName(p); vol != "" {
				return fmt.Errorf("invalid path: %q", p)
			}

			if !windowsValidPath(parts[0]) {
				return fmt.Errorf("invalid path: %q", p)
			}
		}

		if slices.Contains(parts, "..") {
			return fmt.Errorf("invalid path %q: cannot use '..'", p)
		}
	}
	return nil
}

// defaultProtectNTFS returns the default value for core.protectNTFS
// when not explicitly configured. Matches upstream Git behaviour:
// enabled by default on Windows.
func defaultProtectNTFS() bool {
	return runtime.GOOS == "windows"
}

// windowsPathReplacer defines the chars that need to be replaced
// as part of windowsValidPath.
var windowsPathReplacer *strings.Replacer

func init() {
	windowsPathReplacer = strings.NewReplacer(" ", "", ".", "")
}

func windowsValidPath(part string) bool {
	if len(part) > 3 && strings.EqualFold(part[:4], GitDirName) {
		// For historical reasons, file names that end in spaces or periods are
		// automatically trimmed. Therefore, `.git . . ./` is a valid way to refer
		// to `.git/`.
		if windowsPathReplacer.Replace(part[4:]) == "" {
			return false
		}

		// For yet other historical reasons, NTFS supports so-called "Alternate Data
		// Streams", i.e. metadata associated with a given file, referred to via
		// `<filename>:<stream-name>:<stream-type>`. There exists a default stream
		// type for directories, allowing `.git/` to be accessed via
		// `.git::$INDEX_ALLOCATION/`.
		//
		// For performance reasons, _all_ Alternate Data Streams of `.git/` are
		// forbidden, not just `::$INDEX_ALLOCATION`.
		if len(part) > 4 && part[4:5] == ":" {
			return false
		}
	}
	return true
}
