package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidPath is returned by ValidTreePath, and wrapped by the
// worktree filesystem wrapper's own path refusals, when a path is not
// safe to materialise into the worktree.
var ErrInvalidPath = fmt.Errorf("invalid path")

// ValidTreePath rejects path strings that, if materialised into a
// worktree, would let an attacker-controlled tree entry escape the
// worktree or rewrite repository metadata. It rejects:
//
//   - empty paths and control bytes (< 0x20, 0x7f);
//   - "." and ".." components, and the NTFS and HFS+ spellings
//     IsDotOrDotDotName folds back to them;
//   - .git, its 8.3 NTFS short name git~1, and their HFS+ and NTFS
//     variants — at every position, not just the root;
//   - Windows volume name prefixes (e.g. C:), which
//     filepath.VolumeName recognises only on Windows.
//
// Both slash forms separate components. Every rule applies regardless
// of core.protectHFS and core.protectNTFS: tree paths are canonical
// UTF-8 with no zero-width characters or NTFS short-name forms, so an
// entry that looks like a disguise is suspicious anywhere. That is
// stricter than C Git, which gates the disguise checks on those two
// settings and can store such names on POSIX.
//
// Windows reserved device names (CON, NUL, etc.) are not policed
// here: they are legitimate filenames on non-Windows and upstream Git
// accepts them. A component of periods alone passes for the same
// reason. The wrapper layer (validPath in package git) applies those
// rules at materialisation time.
//
// A rejected entry interrupts tree iteration and checkout; entries
// yielded before it have already been seen by the caller.
//
// Mirrors upstream Git's verify_path_internal at read-cache.c#L987-L1048
// in tag v2.54.0[1], with protect_hfs and protect_ntfs treated as
// always-on and is_valid_win32_path left to the wrapper.
//
// [1]: https://github.com/git/git/blob/v2.54.0/read-cache.c#L987-L1048
func ValidTreePath(p string) error {
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return fmt.Errorf("%w %q: contains control character", ErrInvalidPath, p)
		}
	}

	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '\\' || r == '/' })
	if len(parts) == 0 {
		return fmt.Errorf("%w: %q", ErrInvalidPath, p)
	}

	// Volume names are not supported, in both formats: \\ and
	// <DRIVE_LETTER>:.
	if vol := filepath.VolumeName(p); vol != "" {
		return fmt.Errorf("%w: %q", ErrInvalidPath, p)
	}

	for _, part := range parts {
		if IsDotOrDotDotName(part) {
			return fmt.Errorf("%w %q: cannot use %q", ErrInvalidPath, p, part)
		}

		if IsDotGitName(part) || IsHFSDotGit(part) || IsNTFSDotGit(part) {
			return fmt.Errorf("%w component: %q", ErrInvalidPath, p)
		}
	}

	return nil
}
