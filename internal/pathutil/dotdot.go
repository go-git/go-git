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
// C Git carries them in POSIX trees and indexes. ValidTreePath and
// the worktree wrapper's validPath therefore accept them, while
// IsDotsOnlyName refuses runs of periods as submodule and reference
// storage names below .git.
func IsDotOrDotDotName(name string) bool {
	return name == "." || name == ".." ||
		IsHFSDotDot(name) || IsNTFSDotDot(name)
}

// IsDotsOnlyName reports whether a nonempty component consists only of
// ASCII periods. Submodule and reference storage names, which become
// directories below .git, apply it on top of IsDotOrDotDotName.
//
// This is a go-git policy, not a claim that NTFS resolves every such
// name to "..": C Git accepts a periods-only submodule name, and
// Win32 trims a trailing period run only for a component that also
// ends in one. Storage names below .git are cheap to constrain and
// have no legitimate periods-only spelling, so they are refused here
// rather than reasoned about per filesystem.
func IsDotsOnlyName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] != '.' {
			return false
		}
	}
	return true
}
