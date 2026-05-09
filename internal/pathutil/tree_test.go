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
		{"reject control char SOH", "a\x01b", true},
		{"reject DEL", "foo\x7fbar", true},

		// Always-on NTFS variants (no flag gate at this layer).
		{"reject .git . trailing", "sub/.git . /x", true},
		{"reject .git:: ADS", ".git::$INDEX_ALLOCATION/x", true},
		{"reject reserved CON", "CON/file", true},
		{"reject reserved NUL", "dir/NUL", true},

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
