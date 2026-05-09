package pathutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWindowsValidPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{".git", true},
		{".git . . .", false},
		{".git ", false},
		{".git  ", false},
		{".git . .", false},
		{".git . .", false},
		{".git::$INDEX_ALLOCATION", false},
		{".git:", false},
		{"CON", false},
		{"con", false},
		{"CON.txt", false},
		{"CON:ads", false},
		{"CON ", false},
		{"PRN", false},
		{"AUX", false},
		{"NUL", false},
		{"COM1", false},
		{"COM9", false},
		{"LPT1", false},
		{"LPT9", false},
		{"CONIN$", false},
		{"CONOUT$", false},
		{"a", true},
		{"a\\b", true},
		{"a/b", true},
		{".gitm", true},
		{"CONNECT", true},
		{"comic", true},
		{"COM", true},
		{"COM0", true},
		{"LPT0", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			got := WindowsValidPath(tc.path)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsNTFSDot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		part            string
		dotgit          string
		shortnamePrefix string
		want            bool
	}{
		// .gitmodules direct match.
		{"plain .gitmodules", ".gitmodules", "gitmodules", "gi7eba", true},
		{"uppercase .GITMODULES", ".GITMODULES", "gitmodules", "gi7eba", true},
		{"mixed case .GitModules", ".GitModules", "gitmodules", "gi7eba", true},
		// NTFS trailing-space / period / ADS variants on .gitmodules.
		{"trailing space", ".gitmodules ", "gitmodules", "gi7eba", true},
		{"trailing dot", ".gitmodules.", "gitmodules", "gi7eba", true},
		{"trailing space and dot", ".gitmodules .", "gitmodules", "gi7eba", true},
		{"trailing many spaces", ".gitmodules   ", "gitmodules", "gi7eba", true},
		{"alternate data stream", ".gitmodules:foo", "gitmodules", "gi7eba", true},
		// 8.3 short-name standard form: gitmod~[1-4].
		{"short ~1", "gitmod~1", "gitmodules", "gi7eba", true},
		{"short ~4", "gitmod~4", "gitmodules", "gi7eba", true},
		{"short uppercase", "GITMOD~1", "gitmodules", "gi7eba", true},
		{"short ~5 not valid", "gitmod~5", "gitmodules", "gi7eba", false},
		{"short with ads", "gitmod~1:foo", "gitmodules", "gi7eba", true},
		// Fall-back short-name keyed on shortnamePrefix.
		{"fallback gi7eba~1", "gi7eba~1", "gitmodules", "gi7eba", true},
		{"fallback gi7eba~12345", "gi7eba~1", "gitmodules", "gi7eba", true},
		{"fallback gi7ebaX missing tilde", "gi7ebaXY", "gitmodules", "gi7eba", false},
		{"fallback gi7eba1 missing tilde", "gi7eba1X", "gitmodules", "gi7eba", false},
		// Negatives.
		{"plain .gitmodulesfoo", ".gitmodulesfoo", "gitmodules", "gi7eba", false},
		{"plain .gitignore is not gitmodules", ".gitignore", "gitmodules", "gi7eba", false},
		{"plain readme.md", "readme.md", "gitmodules", "gi7eba", false},
		{"empty string", "", "gitmodules", "gi7eba", false},
		{"only dot", ".", "gitmodules", "gi7eba", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsNTFSDot(tc.part, tc.dotgit, tc.shortnamePrefix)
			assert.Equal(t, tc.want, got, "IsNTFSDot(%q, %q, %q)", tc.part, tc.dotgit, tc.shortnamePrefix)
		})
	}
}

func TestIsNTFSDotGitmodules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		part string
		want bool
	}{
		{".gitmodules", true},
		{".GITMODULES", true},
		{".gitmodules ", true},
		{".gitmodules.", true},
		{".gitmodules .", true},
		{".gitmodules:foo", true},
		{"gitmod~1", true},
		{"GITMOD~4", true},
		{"gi7eba~1", true},
		{".gitmodulesfoo", false},
		{".gitignore", false},
		{"readme.md", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.part, func(t *testing.T) {
			t.Parallel()
			got := IsNTFSDotGitmodules(tc.part)
			assert.Equal(t, tc.want, got, "IsNTFSDotGitmodules(%q)", tc.part)
		})
	}
}
