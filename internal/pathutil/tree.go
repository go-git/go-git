package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidPath is returned by ValidTreePath when its argument is
// not a safe path to materialise into the worktree.
var ErrInvalidPath = fmt.Errorf("invalid path")

// ValidTreePath rejects path strings that, if materialised into a
// worktree, would let an attacker-controlled tree entry escape the
// worktree or rewrite repository metadata. It rejects:
//
//   - control characters (< 0x20, 0x7f);
//   - empty paths and "." / ".." components;
//   - Windows volume name prefixes (e.g. C:);
//   - .git, its 8.3 NTFS short-name git~1, plus their HFS+ and NTFS
//     variants — at every position, not just the root;
//   - NTFS reserved device names (CON, NUL, etc).
//
// HFS+/NTFS variants are always rejected at this layer regardless of
// runtime config: tree paths are canonical UTF-8 with no zero-width
// characters or NTFS short-name forms; an entry that looks like one
// is suspicious anywhere. The flag-gated checks at the wrapper layer
// (validPath in package git) handle application-supplied paths,
// where some shapes legitimately appear during submodule cleanup.
//
// Mirrors upstream Git's verify_path_internal at read-cache.c#L987
// in tag v2.54.0[1], stripped of its protect_hfs / protect_ntfs
// gating because tree paths are checked at the boundary where data
// leaves the trusted store.
//
// [1]: https://github.com/git/git/blob/v2.54.0/read-cache.c#L987
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

	// Volume names are not supported, in both formats: \\ and <DRIVE_LETTER>:.
	if vol := filepath.VolumeName(p); vol != "" {
		return fmt.Errorf("%w: %q", ErrInvalidPath, p)
	}

	for _, part := range parts {
		if part == "." || part == ".." {
			return fmt.Errorf("%w %q: cannot use %q", ErrInvalidPath, p, part)
		}

		if IsDotGitName(part) || IsHFSDotGit(part) {
			return fmt.Errorf("%w component: %q", ErrInvalidPath, p)
		}

		if !WindowsValidPath(part) {
			return fmt.Errorf("%w: %q", ErrInvalidPath, p)
		}
	}

	return nil
}
