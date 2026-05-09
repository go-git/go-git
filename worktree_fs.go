package git

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// defaultProtectNTFS returns the default value for core.protectNTFS
// when not explicitly configured. Matches upstream Git behaviour:
// enabled by default on Windows.
func defaultProtectNTFS() bool {
	return runtime.GOOS == "windows"
}

// defaultProtectHFS returns the default value for core.protectHFS
// when not explicitly configured. Matches upstream Git behaviour:
// enabled by default on macOS.
func defaultProtectHFS() bool {
	return runtime.GOOS == "darwin"
}

// hfsIgnoredCodepoints contains Unicode code points that HFS+ ignores
// during path normalization. A path containing these characters between
// the characters of ".git" will be treated as ".git" by HFS+.
//
// See upstream Git utf8.c next_hfs_char() for the full list.
var hfsIgnoredCodepoints = map[rune]bool{
	0x200c: true, // ZERO WIDTH NON-JOINER
	0x200d: true, // ZERO WIDTH JOINER
	0x200e: true, // LEFT-TO-RIGHT MARK
	0x200f: true, // RIGHT-TO-LEFT MARK
	0x202a: true, // LEFT-TO-RIGHT EMBEDDING
	0x202b: true, // RIGHT-TO-LEFT EMBEDDING
	0x202c: true, // POP DIRECTIONAL FORMATTING
	0x202d: true, // LEFT-TO-RIGHT OVERRIDE
	0x202e: true, // RIGHT-TO-LEFT OVERRIDE
	0x206a: true, // INHIBIT SYMMETRIC SWAPPING
	0x206b: true, // ACTIVATE SYMMETRIC SWAPPING
	0x206c: true, // INHIBIT ARABIC FORM SHAPING
	0x206d: true, // ACTIVATE ARABIC FORM SHAPING
	0x206e: true, // NATIONAL DIGIT SHAPES
	0x206f: true, // NOMINAL DIGIT SHAPES
	0xfeff: true, // ZERO WIDTH NO-BREAK SPACE
}

// isHFSDotGit returns true if the given path component would be
// treated as ".git" on an HFS+ filesystem after stripping ignored
// Unicode code points and folding to lower case.
func isHFSDotGit(part string) bool {
	const needle = "git"

	runes := []rune(part)
	i := 0

	// skip ignored code points, then expect '.'
	for i < len(runes) && hfsIgnoredCodepoints[runes[i]] {
		i++
	}
	if i >= len(runes) || runes[i] != '.' {
		return false
	}
	i++

	// match "git" case-insensitively, skipping ignored code points
	for _, expected := range needle {
		for i < len(runes) && hfsIgnoredCodepoints[runes[i]] {
			i++
		}
		if i >= len(runes) {
			return false
		}
		r := runes[i]
		if r > 127 {
			return false
		}
		if strings.ToLower(string(r)) != string(expected) {
			return false
		}
		i++
	}

	// skip trailing ignored code points
	for i < len(runes) && hfsIgnoredCodepoints[runes[i]] {
		i++
	}

	// must be at end of component
	return i == len(runes)
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
	return !isWindowsReservedName(part)
}

// windowsReservedNames lists the Windows reserved device names.
// A path component is reserved if its base name (ignoring trailing
// spaces, extensions, and NTFS Alternate Data Streams) matches one of
// these case-insensitively.
//
// See upstream Git compat/mingw.c is_valid_win32_path().
var windowsReservedNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	"CONIN$", "CONOUT$",
}

func isWindowsReservedName(part string) bool {
	for _, name := range windowsReservedNames {
		if len(part) < len(name) {
			continue
		}
		if !strings.EqualFold(part[:len(name)], name) {
			continue
		}
		// Exact match or followed by space, dot, colon (ADS), or separator.
		if len(part) == len(name) {
			return true
		}
		switch part[len(name)] {
		case ' ', '.', ':':
			return true
		}
	}
	return false
}
