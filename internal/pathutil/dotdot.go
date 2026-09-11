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
// the worktree wrapper's validPath therefore accept them. Below .git
// the policy is stricter: IsDotsOnlyName refuses runs of periods as
// submodule and reference storage names, and IsDotName refuses the
// spellings that fold to ".". On a Win32 host with core.protectNTFS,
// validPath additionally consults Win32ValidPath, which refuses a
// component ending in a space or a period.
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

// IsDotName reports whether one path component names the current
// directory. It matches the literal ".", and the NTFS and HFS+
// spellings those filesystems fold back to "." — ". ", ". .", ".:",
// ".<U+200C>" and "<U+200C>.". IsDotOrDotDotName owns the parent
// equivalents.
//
// Runs of periods ("...", "....") are excluded, for the reason
// IsDotOrDotDotName excludes them: C Git carries them in POSIX trees
// and indexes. IsDotsOnlyName refuses them as storage names below
// .git.
//
// A component that folds to "." resolves to the directory holding it,
// so a path built from one addresses that directory rather than
// something below it. Submodule paths and storage names apply this;
// tree and worktree paths do not, where such a component only names
// an ordinary file.
func IsDotName(name string) bool {
	return name == "." || IsHFSDotCurrent(name) || IsNTFSDotCurrent(name)
}

// IsUnsafeStorageName reports whether one component is unsafe as a
// directory name below .git, where submodule storage paths are built.
// It composes the periods-only, parent and current-directory policies
// so every caller that resolves a name to repository metadata applies
// the same set.
//
// This is stricter than C Git, whose check_submodule_name refuses only
// an empty name and a literal ".." component. Storage names below .git
// have no legitimate spelling among these, so they are refused here
// rather than reasoned about per filesystem.
//
// Reference: upstream Git check_submodule_name at
// submodule-config.c#L214-L237 in tag v2.54.0[1].
//
// [1]: https://github.com/git/git/blob/v2.54.0/submodule-config.c#L214-L237
func IsUnsafeStorageName(part string) bool {
	return IsDotsOnlyName(part) ||
		IsDotOrDotDotName(part) ||
		IsDotName(part)
}
