package pathutil

// IsDotOrDotDotName reports whether one path component names the
// current or the parent directory. It takes a single component, never
// a whole path. It matches the literal "." and "..", and the NTFS and
// HFS+ spellings those filesystems fold back to "..". Tree, worktree,
// submodule and reference validation share it, so widening it widens
// every gate at once.
//
// Runs of periods ("...", "....") and spellings that fold to "."
// rather than ".." (". ", ". .", ".<U+200C>") are excluded, because
// C Git carries them in POSIX trees and indexes.
func IsDotOrDotDotName(name string) bool {
	return name == "." || name == ".." ||
		IsHFSDotDot(name) || IsNTFSDotDot(name)
}
