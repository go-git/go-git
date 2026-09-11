package pathutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidTreePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		// Strict positional rejection at every component.
		{"reject submodule/.git", "submodule/.git", true},
		{"reject a/.git", "a/.git", true},
		{"reject a\\.git", "a\\.git", true},
		{"reject root .git", ".git", true},
		{"reject .git/config", ".git/config", true},
		{"reject a/.git/b", "a/.git/b", true},
		{"reject git~1", "git~1", true},
		{"reject sub/git~1/HEAD", "sub/git~1/HEAD", true},
		{"reject ..", "a/../b", true},
		{"reject .", ".", true},
		{"reject empty", "", true},

		// NTFS parent disguises remain an always-on policy.
		{"reject .. trailing space", ".. /x", true},
		{"reject nested .. trailing space", "a/.. /b", true},
		// A space in the tail distinguishes this from periods alone.
		{"reject .. periods then spaces", "...  /x", true},
		{"reject ..:: ADS", "..::$INDEX_ALLOCATION/x", true},
		// HFS+ ignores certain code points, so these resolve to "..".
		{"reject .zwnj. hfs", ".\u200c./x", true},
		{"reject nested .zwnj. hfs", "a/.\u200c./b", true},
		{"reject ..:$DATA ADS", "..:$DATA/x", true},
		{"reject ..:x ADS", "..:x/x", true},
		{"reject ..zwnj hfs", "..\u200c/x", true},
		{"reject zwnj.. hfs", "\u200c../x", true},
		// ValidTreePath treats '\' as a separator too, so a disguise
		// behind a backslash must be refused as well.
		{"reject backslash .. trailing space", "a\\.. \\b", true},
		{"reject backslash .zwnj. hfs", "a\\.\u200c.\\b", true},
		{"reject control char SOH", "a\x01b", true},
		{"reject DEL", "foo\x7fbar", true},

		// Always-on NTFS .git-disguise variants (no flag gate at this layer).
		{"reject .git . trailing", "sub/.git . /x", true},
		{"reject .git:: ADS", ".git::$INDEX_ALLOCATION/x", true},
		{"reject git~1 trailing space", "sub/git~1 /x", true},
		{"reject git~1 trailing dot", "git~1./x", true},
		{"reject git~1:: ADS", "git~1::$INDEX_ALLOCATION/x", true},

		// Always-on HFS+ variants.
		{"reject .g\u200cit zwnj", ".g\u200cit/x", true},
		{"reject sub/.g\u200cit zwnj", "sub/.g\u200cit/x", true},

		// Legitimate paths pass.
		{"allow readme.md", "readme.md", false},
		{"allow src/main.go", "src/main.go", false},
		{"allow .gitmodules", ".gitmodules", false},
		{"allow .gitignore", ".gitignore", false},
		{"allow nested .gitignore", "vendor/.gitignore", false},
		{"allow a..b", "a..b", false},
		{"allow submodule directory entry", "submodule", false},
		{"allow nested submodule directory", "vendor/sub", false},
		{"allow Çircle/file high-codepoint", "Çircle/file", false},
		// Windows reserved device names are not policed at this layer:
		// they are legitimate on non-Windows and upstream Git accepts
		// them. The wrapper enforces them on Windows with core.protectNTFS.
		{"allow CON file", "CON/file", false},
		{"allow CON.txt", "CON.txt", false},
		{"allow nested NUL", "dir/NUL", false},
		// A component of periods alone is well-formed on filesystems
		// other than NTFS and C Git 2.54.0 accepts it in a tree and
		// in an index on POSIX. Win32ValidPath refuses it on Windows
		// when core.protectNTFS is enabled.
		{"allow ... component", ".../x", false},
		{"allow .... component", "....", false},
		{"allow nested ...", "a/.../b", false},
		{"allow x..", "x../y", false},
		{"allow foo..", "foo..", false},
		{"allow ..x", "..x/y", false},
		{"allow .. x", ".. x", false},
		// These fold to "." rather than to "..", so they cause no
		// parent hop and are deliberately accepted.
		{"allow . space", ". /x", false},
		{"allow . space .", ". .", false},
		{"allow .zwnj", ".\u200c", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidTreePath(tc.path)
			if tc.wantErr {
				assert.Error(t, err, "ValidTreePath(%q) should return error", tc.path)
			} else {
				assert.NoError(t, err, "ValidTreePath(%q) should not return error", tc.path)
			}
		})
	}
}

func TestValidSubmodulePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		// Everything ValidTreePath refuses stays refused.
		{"reject .git component", "sub/.git", true},
		{"reject parent", "a/../b", true},
		{"reject parent disguise", "a/.. /b", true},
		{"reject control byte", "a/\x01/b", true},
		{"reject empty", "", true},

		// The rule this adds: a component the filesystem folds to
		// ".". Submodule.Repository chroots to this path, so such a
		// component scopes the submodule to the directory above it.
		{"reject dot", ".", true},
		{"reject dot trailing space", ". ", true},
		{"reject dot space period", ". .", true},
		{"reject dot ADS", ".:$DATA", true},
		{"reject dot zwnj hfs", ".\u200c", true},
		{"reject zwnj dot hfs", "\u200c.", true},
		{"reject nested dot trailing space", "sub/. /x", true},
		{"reject trailing dot component", "sub/.\u200c", true},

		// Runs of periods stay valid, as they do in a tree: C Git
		// carries them on POSIX and ValidTreePath accepts them.
		{"accept periods only", "...", false},
		{"accept four periods", "....", false},
		{"accept nested periods only", "a/.../b", false},
		{"accept trailing periods", "x..", false},
		{"accept leading periods", "..x", false},
		{"accept periods then space", ".. x", false},

		// Ordinary submodule paths.
		{"accept plain", "sub", false},
		{"accept nested", "deps/sub", false},
		{"accept dotfile", "sub/.gitignore", false},
		{"accept period in name", "deps/x.y", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidSubmodulePath(tc.path)
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrInvalidPath, "path %q", tc.path)
				return
			}
			assert.NoError(t, err, "path %q", tc.path)
		})
	}
}

// ValidSubmodulePath is ValidTreePath plus one rule, so it must never
// accept a path the tree gate refuses.
func TestValidSubmodulePathIsStricterThanValidTreePath(t *testing.T) {
	t.Parallel()

	for _, part := range generateComponents(t, "./ :a\u200c", 3) {
		if ValidTreePath(part) != nil {
			assert.Error(t, ValidSubmodulePath(part), "path %q", part)
		}
	}
}
