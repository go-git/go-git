package dotgit

import (
	"errors"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/internal/pathutil"
)

// ErrModuleNameInvalid is returned by [DotGit.Module] when a submodule
// name stays below the modules/ subtree but cannot name a directory
// there: it holds a NUL or ends in a separator, or, on Windows under
// core.protectNTFS, one of its components is not a valid Win32 file name.
// A name that addresses modules/ itself or a directory outside it returns
// [ErrModuleNameEscape] instead.
var ErrModuleNameInvalid = errors.New("submodule name is not a valid storage name")

// checkModuleName refuses a storage name Module must not use. win32
// applies the rules of a Windows host, so both hosts' policies can be
// tested on either.
//
// Creating the directory on Windows needs a valid Win32 name, as Git's
// mingw mkdir wrapper requires under core.protectNTFS, its default[1].
// With the setting off, Git's is_valid_win32_path accepts any name[2],
// and so does Module. The .gitmodules parser does not apply this check,
// as Git lists such a submodule.
//
// [1]: https://github.com/git/git/blob/v2.54.0/compat/mingw.c#L597-L606
// [2]: https://github.com/git/git/blob/v2.54.0/compat/mingw.c#L3347-L3353
func (d *DotGit) checkModuleName(name string, win32 bool) error {
	if !pathutil.ValidStorageName(name) {
		if moduleNameEscapes(name) {
			return ErrModuleNameEscape
		}
		return ErrModuleNameInvalid
	}
	if !win32 {
		return nil
	}
	for part := range strings.FieldsFuncSeq(name, isModuleNameSep) {
		// The configuration is read only for a name the check refuses,
		// so an ordinary name costs no read.
		if !pathutil.Win32ValidPath(part) && d.protectNTFS() {
			return ErrModuleNameInvalid
		}
	}
	return nil
}

// moduleNameEscapes reports whether a name pathutil.ValidStorageName
// refuses would address modules/ itself or a directory outside it: an
// empty name, a leading separator or drive prefix, or a component naming
// "." or "..", literally or in an NTFS or HFS+ spelling. The other names
// it refuses, with a NUL or a trailing separator, stay below modules/.
func moduleNameEscapes(name string) bool {
	if name == "" || isModuleNameSep(rune(name[0])) || (len(name) >= 2 && name[1] == ':') {
		return true
	}
	for part := range strings.FieldsFuncSeq(name, isModuleNameSep) {
		if pathutil.IsDotOrDotDotName(part) || pathutil.IsDotName(part) {
			return true
		}
	}
	return false
}

func isModuleNameSep(r rune) bool { return r == '/' || r == '\\' }

// protectNTFS reports core.protectNTFS as the repository's configuration
// sets it, with a config.worktree overlaying it as ConfigStorage does.
// Unset, it is true, Git's default on every host. A configuration that
// cannot be read counts as unset, so the check stays on.
func (d *DotGit) protectNTFS() bool {
	v, worktreeConfig := readProtectNTFS(d.Config)
	if worktreeConfig {
		if wv, _ := readProtectNTFS(d.ConfigWorktree); wv.IsSet() {
			v = wv
		}
	}
	return !v.IsSet() || v.IsTrue()
}

// readProtectNTFS reads core.protectNTFS and extensions.worktreeConfig
// from the file open returns. A missing or unreadable file reports both
// unset.
func readProtectNTFS(open func() (billy.File, error)) (config.OptBool, bool) {
	f, err := open()
	if err != nil {
		return config.OptBoolUnset, false
	}
	defer func() { _ = f.Close() }()
	cfg, err := config.ReadConfig(f)
	if err != nil {
		return config.OptBoolUnset, false
	}
	return cfg.Core.ProtectNTFS, cfg.Extensions.WorktreeConfig
}
