package pathutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWin32ValidPath(t *testing.T) {
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
		{"git~1 ", false},
		{"git~1.", false},
		{"GIT~1 ", false},
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
		{"LPT0", false},
		{"LPT1", false},
		{"LPT9", false},
		{"CONIN$", false},
		{"CONOUT$", false},
		{"lPt0.txt", false},
		{"CON .txt", false},
		{"AUX  .log", false},
		{"CONIN$ .txt", false},
		{"CONOUT$ .txt", false},
		{"foo:bar", false},
		{"foo::$DATA", false},
		{"foo<bar", false},
		{"foo>bar", false},
		{"foo\"bar", false},
		{"foo|bar", false},
		{"foo?bar", false},
		{"foo*bar", false},
		// Upstream's is_valid_win32_path refuses a component ending
		// in a space or a period, excepting exactly "." and "..".
		// Win32 canonicalisation strips both characters, so such a
		// component names something other than what it spells.
		{"foo ", false},
		{"foo.", false},
		{"foo  ", false},
		{"foo ..", false},
		{"sub ", false},
		{".gitattributes ", false},
		{".gitignore ", false},
		{".gitmodules.", false},
		{"...", false},
		{"....", false},
		{". ", false},
		{". .", false},
		{".. ", false},
		// The two upstream exceptions. NTFS resolves these to the
		// directory itself and to its parent, which is the caller's
		// business, not a spelling problem.
		{".", true},
		{"..", true},
		{"a", true},
		{"a\\b", true},
		{"a/b", true},
		{".gitm", true},
		{"CONNECT", true},
		{"comic", true},
		{"COM", true},
		{"COM0", true},
		{"LPT10", true},
		{"con c", true},
		{"aux b", true},
		{"prn x", true},
		{"nul x", true},
		{"COM1 port", true},
		{"LPT0 port", true},
		{"CONIN$ input", true},
		{"CONOUT$ output", true},
		{"foo\x7fbar", true}, // The worktree's always-on control check rejects DEL.
		{"résumé.txt", true},
		// Bare ".git" / "git~1" stay valid here; the caller decides
		// whether they are permissible at the current path position.
		{".git", true},
		{"git~1", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			got := Win32ValidPath(tc.path)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWin32ValidPathControlCharacters(t *testing.T) {
	t.Parallel()
	for c := range byte(0x20) {
		name := "foo" + string(c) + "bar"
		assert.False(t, Win32ValidPath(name), "component %q", name)
	}
}

func TestIsNTFSDotGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		part string
		want bool
	}{
		// Bare canonical names match (parity with upstream).
		{".git", true},
		{".GIT", true},
		{"git~1", true},
		{"GIT~1", true},
		// Trailing-space / period / ADS variants on .git.
		{".git ", true},
		{".git.", true},
		{".git . . .", true},
		{".git::$INDEX_ALLOCATION", true},
		{".git:foo", true},
		// Same shapes on git~1.
		{"git~1 ", true},
		{"git~1.", true},
		{"git~1 . ", true},
		{"git~1::$DATA", true},
		{"GIT~1.", true},
		// Negatives.
		{".gitignore", false},
		{".gitmodules", false},
		{"gitfoo", false},
		{"git~2", false},
		{"git~10", false},
		{"git", false},
		{"", false},
		{".", false},
		{"readme.md", false},
	}

	for _, tc := range tests {
		t.Run(tc.part, func(t *testing.T) {
			t.Parallel()
			got := IsNTFSDotGit(tc.part)
			assert.Equal(t, tc.want, got, "IsNTFSDotGit(%q)", tc.part)
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

func TestIsNTFSDotMetadataFamily(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		fn        func(string) bool
		canonical string
		short     string
	}{
		{"IsNTFSDotGitattributes", IsNTFSDotGitattributes, ".gitattributes", "gi7d29~1"},
		{"IsNTFSDotGitignore", IsNTFSDotGitignore, ".gitignore", "gi250a~1"},
		{"IsNTFSDotMailmap", IsNTFSDotMailmap, ".mailmap", "maba30~1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, tc.fn(tc.canonical), "%s(%q)", tc.name, tc.canonical)
			assert.True(t, tc.fn(tc.canonical+" "), "%s(%q)", tc.name, tc.canonical+" ")
			assert.True(t, tc.fn(tc.canonical+":foo"), "%s(%q)", tc.name, tc.canonical+":foo")
			assert.True(t, tc.fn(tc.short), "%s(%q)", tc.name, tc.short)
			assert.False(t, tc.fn(".gitmodules"), "%s(%q)", tc.name, ".gitmodules")
			assert.False(t, tc.fn(""), "%s(empty)", tc.name)
		})
	}
}

// TestWin32ValidPathTrailingRuleMatchesUpstream compares the
// trailing-space/period rule against a direct transcription of
// upstream Git's segment-boundary condition in is_valid_win32_path
// at compat/mingw.c#L3363-L3366 in tag v2.54.0[1], over every
// component in a closed corpus. A table of hand-picked rows catches
// a wrong verdict; only this catches a subtly wrong port.
//
// [1]: https://github.com/git/git/blob/v2.54.0/compat/mingw.c#L3363-L3366
func TestWin32ValidPathTrailingRuleMatchesUpstream(t *testing.T) {
	t.Parallel()

	// upstreamRejects transcribes
	//   preceding_space_or_period && (i != periods || periods > 2)
	// where i is the component length in bytes and periods is the
	// count of periods in it.
	upstreamRejects := func(part string) bool {
		periods, precedingSpaceOrPeriod := 0, false
		for i := 0; i < len(part); i++ {
			switch part[i] {
			case '.':
				periods++
				precedingSpaceOrPeriod = true
			case ' ':
				precedingSpaceOrPeriod = true
			default:
				precedingSpaceOrPeriod = false
			}
		}
		return precedingSpaceOrPeriod &&
			(len(part) != periods || periods > 2)
	}

	for _, part := range generateComponents(t, ". a:", 5) {
		assert.Equal(t, upstreamRejects(part), endsInSpaceOrPeriod(part),
			"component %q", part)
	}
}
