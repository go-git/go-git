package pathutil

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
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
		// The hash-style short name keeps eight characters however long
		// its number grows, at the expense of the prefix.
		{"hashed with two digits", "gi7eb~12", "gitmodules", "gi7eba", true},
		{"hashed with three digits", "GI7E~123", "gitmodules", "gi7eba", true},
		{"hashed with number only", "~1234567", "gitmodules", "gi7eba", true},
		{"hashed number starting at zero", "gi7eba~0", "gitmodules", "gi7eba", false},
		{"hashed with a later zero", "gi7e~103", "gitmodules", "gi7eba", true},
		{"hashed with a letter in the number", "gi7e~1a3", "gitmodules", "gi7eba", false},
		{"hashed followed by text", "gi7eba~1x", "gitmodules", "gi7eba", false},
		{"hashed followed by spaces and stream", "gi7eba~1 . :x", "gitmodules", "gi7eba", true},
		{"hashed one character short", "gi7e~12", "gitmodules", "gi7eba", false},
		{"hashed with non-ASCII byte", "gi7e\xe9~12", "gitmodules", "gi7eba", false},
		{"hashed without a prefix to match", "gi7eba~1", "gitmodules", "", false},
		{"short ~0 not valid", "gitmod~0", "gitmodules", "gi7eba", false},
		{"short with trailing period", "gitmod~4.", "gitmodules", "gi7eba", true},
		{"short followed by a digit", "gitmod~12", "gitmodules", "gi7eba", false},
		{"short of a name under six characters", "git~1", "git", "", false},
		{"stream after spaces and periods", ".gitmodules . :x", "gitmodules", "gi7eba", true},
		{"stream named $DATA", ".gitmodules::$DATA", "gitmodules", "gi7eba", true},
		{"extension", ".gitmodules.x", "gitmodules", "gi7eba", false},
		{"leading text", "x.gitmodules", "gitmodules", "gi7eba", false},
		{"long s is not an s", ".gitmodule\u017f", "gitmodules", "gi7eba", false},
		// An empty name stands for the directory itself, "." for its parent.
		{"current directory", ".", "", "", true},
		{"current directory with space", ". ", "", "", true},
		{"current directory with stream", ".:x", "", "", true},
		{"run of periods", "...", "", "", true},
		{"empty component", "", "", "", false},
		{"space before the period", " .", "", "", false},
		{"period then text", ".a", "", "", false},
		{"parent directory", "..", ".", "", true},
		{"parent directory with space", ".. ", ".", "", true},
		{"parent directory with stream", "..::$INDEX_ALLOCATION", ".", "", true},
		{"single period is not the parent", ".", ".", "", false},
		{"period and space is not the parent", ". .", ".", "", false},
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

// TestIsNTFSDotMatchesGit stores each candidate name as a symbolic link in
// a tree of its own and checks that the four metadata predicates flag the
// names git fsck reports as a symlinked .gitmodules, .gitattributes,
// .gitignore or .mailmap. The candidates are ASCII apart from a few bytes
// that must not match, so the HFS+ rules fsck applies as well stay out of
// the picture.
func TestIsNTFSDotMatchesGit(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("oracle disabled: -short")
	}
	gitenv.RequireAtLeast(t, 2, 32, "fsck reporting symlinked .gitattributes, .gitignore and .mailmap")

	families := []struct {
		dotgit, shortnamePrefix, fsckMessage string
		isAlias                              func(string) bool
	}{
		{"gitmodules", "gi7eba", "gitmodulesSymlink", IsNTFSDotGitmodules},
		{"gitattributes", "gi7d29", "gitattributesSymlink", IsNTFSDotGitattributes},
		{"gitignore", "gi250a", "gitignoreSymlink", IsNTFSDotGitignore},
		{"mailmap", "maba30", "mailmapSymlink", IsNTFSDotMailmap},
	}

	candidates := map[string]struct{}{}
	for _, f := range families {
		long, six, hashed := "."+f.dotgit, f.dotgit[:6], f.shortnamePrefix
		for _, c := range []string{
			long, strings.ToUpper(long), long + ".", long + "  ", long + "::$DATA", long + " . :x",
			long + ":", long + "x", long + ".x", long + " x", "x" + long, f.dotgit, " " + long,
			strings.Replace(long, "s", "ſ", 1), long[:len(long)-1],

			six + "~1", six + "~2", six + "~3", six + "~4", six + "~0", six + "~5", six + "~9",
			strings.ToUpper(six) + "~1", six + "~4.", six + "~1:stream", six + "~1 . ", six + "~1 . :x",
			six + "~12", six + "~1x", six + "~", six + "x1", six[:5] + "~1", six[:5] + "~12",
			six[:5] + "\xe9~1", "." + six + "~1",

			hashed + "~1", strings.ToUpper(hashed) + "~9", hashed + "~0", hashed + "X1", hashed + "~1x",
			hashed + "~1.", hashed + "~1:x", hashed + "~1 :x", hashed + "~", hashed + "~~",
			hashed[:5] + "~12", hashed[:5] + "~1", hashed[:5] + "~02", hashed[:5] + "~10",
			hashed[:4] + "~123", hashed[:4] + "~023", hashed[:4] + "~103", hashed[:4] + "~1a3",
			hashed[:4] + "~12", hashed[:4] + "~1234", hashed[:4] + "~~12", hashed[:4] + "~1~2",
			hashed[:4] + "x~12", "x" + hashed[:4] + "~12", hashed[:4] + "\xe9~12",
			hashed[:3] + "~1234", hashed[:2] + "~12345", hashed[:1] + "~123456",
			hashed[1:] + "~12", hashed[:2] + hashed[3:] + "~12",
		} {
			candidates[c] = struct{}{}
		}
	}
	for _, c := range []string{
		"~1234567", "~0234567", "~123456", "~12345678", "~1234567.", "~1234567:x", "~123456x",
		"\xe9~123456", "12345678", "~~~~~~~~", "readme.md", "~1", ".git",
	} {
		candidates[c] = struct{}{}
	}

	dir := t.TempDir()
	runGit := func(stdin string, args ...string) string {
		t.Helper()

		cmd := gitenv.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
		return string(out)
	}

	runGit("", "init", "-q", "--bare", ".")
	blob := strings.TrimSpace(runGit("target", "hash-object", "-w", "--stdin"))
	nameOfTree := map[string]string{}
	for name := range candidates {
		tree := strings.TrimSpace(runGit("120000 blob "+blob+"\t"+name+"\x00", "mktree", "-z"))
		nameOfTree[tree] = name
	}

	// "error in tree <id>: gitmodulesSymlink: ..." or "warning in tree ...".
	reported := map[string]map[string]bool{}
	for line := range strings.Lines(runGit("", "-c", "fsck.gitmodulesSymlink=warn", "fsck", "--no-dangling")) {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[1] != "in" || fields[2] != "tree" {
			continue
		}
		name, ok := nameOfTree[strings.TrimSuffix(fields[3], ":")]
		require.True(t, ok, "fsck reported an unknown tree: %s", line)
		message := strings.TrimSuffix(fields[4], ":")
		if reported[message] == nil {
			reported[message] = map[string]bool{}
		}
		reported[message][name] = true
	}

	for _, f := range families {
		require.NotEmpty(t, reported[f.fsckMessage], "git fsck reported no %s", f.fsckMessage)
		for name := range candidates {
			assert.Equal(t, reported[f.fsckMessage][name], f.isAlias(name),
				"%q as an alias of .%s", name, f.dotgit)
		}
	}
}

// TestIsNTFSDotGitMatchesGit stores each candidate name in a tree of its own
// and checks that IsNTFSDotGit flags the names git fsck reports as a ".git"
// entry. The candidates are ASCII, so the HFS+ rules fsck applies as well
// flag none of them that the NTFS rules do not.
func TestIsNTFSDotGitMatchesGit(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("oracle disabled: -short")
	}
	gitenv.RequireAtLeast(t, 2, 32, "the fsck report format this test reads")

	candidates := make([]string, 0, 160)
	for _, name := range []string{".git", ".GIT", ".Git", "git~1", "GIT~1", "Git~1"} {
		for _, tail := range []string{
			"", ".", " ", "..", "  ", " . ", ". .", ":", ":x", "::$DATA", " :x", ". :x", " . :", ".:",
			"x", " x", ".x", "x.", "x:", " x:y", "~1", "2",
		} {
			candidates = append(candidates, name+tail)
		}
	}
	candidates = append(candidates,
		"git", ".gi", ".gitt", "x.git", " .git", "..git", ":.git", "git~", "git~2", "git~0", "git~10",
		"gi~1", ".git~1", "git ~1", "git~ 1", "g:it~1", ".g:it", ".gitmodules", "readme.md", "~1",
	)

	dir := t.TempDir()
	runGit := func(stdin string, args ...string) string {
		t.Helper()

		cmd := gitenv.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
		return string(out)
	}

	runGit("", "init", "-q", "--bare", ".")
	blob := strings.TrimSpace(runGit("content", "hash-object", "-w", "--stdin"))
	nameOfTree := map[string]string{}
	for _, name := range candidates {
		tree := strings.TrimSpace(runGit("100644 blob "+blob+"\t"+name+"\x00", "mktree", "-z"))
		nameOfTree[tree] = name
	}

	// "warning in tree <id>: hasDotgit: contains '.git'".
	reported := map[string]bool{}
	for line := range strings.Lines(runGit("", "fsck", "--no-dangling")) {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[1] != "in" || fields[2] != "tree" || fields[4] != "hasDotgit:" {
			continue
		}
		name, ok := nameOfTree[strings.TrimSuffix(fields[3], ":")]
		require.True(t, ok, "fsck reported an unknown tree: %s", line)
		reported[name] = true
	}
	require.NotEmpty(t, reported, "git fsck reported no hasDotgit")

	for _, name := range candidates {
		assert.Equal(t, reported[name], IsNTFSDotGit(name), "IsNTFSDotGit(%q)", name)
	}
}
