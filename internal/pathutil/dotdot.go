package pathutil

import "strings"

// IsDotOrDotDotName reports whether one path component names the
// current or the parent directory. It takes a single component, never
// a whole path. It matches the literal "." and "..", and the NTFS and
// HFS+ spellings those filesystems fold back to "..". Submodule paths and
// storage names apply it; tree and worktree paths refuse only the literal
// names, as Git's verify_dotfile does.
//
// Runs of periods ("...", "....") and spellings that fold to "."
// rather than ".." (". ", ". .", ".<U+200C>") are excluded. IsDotName
// owns the spellings of ".".
func IsDotOrDotDotName(name string) bool {
	return name == "." || name == ".." ||
		IsHFSDotDot(name) || IsNTFSDotDot(name)
}

// IsDotName reports whether one path component names the current
// directory. It matches the literal ".", and the NTFS and HFS+
// spellings those filesystems fold back to "." — ". ", ". .", ".:",
// ".<U+200C>" and "<U+200C>.". IsDotOrDotDotName owns the parent
// equivalents.
//
// Runs of periods ("...", "....") are excluded, as C Git carries them in
// POSIX trees and indexes.
//
// A component that folds to "." resolves to the directory holding it,
// so a path built from one addresses that directory rather than
// something below it. Submodule paths and storage names apply this;
// tree and worktree paths do not, where such a component only names
// an ordinary file.
func IsDotName(name string) bool {
	return name == "." || IsHFSDotCurrent(name) || IsNTFSDotCurrent(name)
}

// ValidStorageName reports whether name is safe as a submodule's storage
// name, the path below .git/modules where its Git directory is kept. It
// refuses an empty name, a NUL, a leading or trailing separator, a drive
// prefix, and a component that names the current or the parent directory,
// literally or in an NTFS or HFS+ spelling. Both separators are recognized
// on every host. A run of periods, such as "...", is accepted, as in Git.
//
// Upstream Git's check_submodule_name[1] refuses only an empty name and a
// literal ".." component. The rest is a deliberate go-git policy, stricter
// than Git, that keeps a name from addressing anything but a directory
// below .git/modules. It applies on every host, whatever core.protectNTFS
// and core.protectHFS say.
//
// [1]: https://github.com/git/git/blob/v2.54.0/submodule-config.c#L214-L237
func ValidStorageName(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) {
		return false
	}
	if isPathSep(rune(name[0])) || isPathSep(rune(name[len(name)-1])) {
		return false
	}
	if len(name) >= 2 && name[1] == ':' {
		return false
	}
	for _, part := range strings.FieldsFunc(name, isPathSep) {
		if IsDotOrDotDotName(part) || IsDotName(part) {
			return false
		}
	}
	return true
}
