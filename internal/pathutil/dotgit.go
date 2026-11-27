package pathutil

import "strings"

// IsDotGitName reports whether name is `.git` or its 8.3 NTFS short
// alias `git~1`, case-insensitively. Both are forbidden as path
// components (and as submodule names) because they refer to the
// repository's own metadata directory.
//
// NTFS can give a file whose name does not fit the 8.3 pattern a second,
// generated short name, and usually does so on the system drive. For
// `.git` that short name is `git~1`, so either spelling reaches the
// same directory.
func IsDotGitName(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "git~1":
		return true
	}
	return false
}
