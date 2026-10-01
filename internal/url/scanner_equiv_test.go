package url

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"runtime"
	"testing"
)

// The oracle expressions specify the grammars used by the URL scanners.
// The differential tests compare match results and extracted components.
var oracleScheme = regexp.MustCompile(`://`)

// Independent from the fuzz target, whose helpers must remain local.
var (
	bracketStart = regexp.MustCompile(`@\[`)
	bracketHost  = regexp.MustCompile(`^\[[^\]]*\]`)
	hostPath     = regexp.MustCompile(`(?s)^([^:]*):(.*)$`)
	userHost     = regexp.MustCompile(`(?s)^(.*)@([^@]*)$`)
)

//nolint:dupl // OSS-Fuzz requires a separate oracle inside the fuzz target.
func oracleSCPComponents(s string) []string {
	start := 0
	if loc := bracketStart.FindStringIndex(s); loc != nil {
		start = loc[0] + 1
	}
	offset := 0
	if loc := bracketHost.FindStringIndex(s[start:]); loc != nil {
		offset = start + loc[1]
	}
	m := hostPath.FindStringSubmatch(s[offset:])
	if m == nil {
		return nil
	}
	authority := s[:offset+len(m[1])]
	start = 0
	if loc := bracketStart.FindStringIndex(authority); loc != nil {
		start = loc[0] + 1
	}
	if loc := bracketHost.FindStringIndex(authority[start:]); loc != nil {
		user := ""
		if start > 0 {
			user = authority[:start-1]
		}
		return []string{s, user, authority[start : start+loc[1]], m[2]}
	}
	if parts := userHost.FindStringSubmatch(authority); parts != nil {
		return []string{s, parts[1], parts[2], m[2]}
	}
	return []string{s, "", authority, m[2]}
}

// equivDiff returns a description of the first difference between the
// scanners and the oracle expressions, or an empty string if they agree.
func equivDiff(s string) string {
	if got, want := MatchesScheme(s), oracleScheme.MatchString(s); got != want {
		return fmt.Sprintf("MatchesScheme(%q) = %v, regexp says %v", s, got, want)
	}

	user, host, path, ok := matchScpLike(s)
	m := oracleSCPComponents(s)
	if ok != (m != nil) {
		return fmt.Sprintf("matchScpLike(%q) ok = %v, regexp says %v", s, ok, m != nil)
	}
	if !ok {
		return ""
	}
	if user != m[1] || host != m[2] || path != m[3] {
		return fmt.Sprintf("matchScpLike(%q) = (user=%q host=%q path=%q), regexp = (user=%q host=%q path=%q)",
			s, user, host, path, m[1], m[2], m[3])
	}

	return ""
}

func checkEquiv(t *testing.T, s string) {
	t.Helper()

	if diff := equivDiff(s); diff != "" {
		t.Fatal(diff)
	}
}

// wide reports whether GOGIT_URL_SWEEP is set to "wide", which enables
// larger exhaustive and random test runs.
func wide() bool { return os.Getenv("GOGIT_URL_SWEEP") == "wide" }

// equivSeeds contains inputs covering the URL scanners' grammar rules.
// The OSS-Fuzz target defines its own corpus so it can build independently
// of this file.
var equivSeeds = []string{
	// Scheme detection.
	"", ":", "://", "a://", "a://b", "a:b://c", "://a", "a:/b", "a:",
	"git@host:a://b", "/abs/a://b", "./foo://bar",
	"ssh://git@github.com/user/repository.git",
	"http://git:pass@github.com:8080/user/repository.git?foo#bar",

	// User group boundaries.
	"a@b:c", "a@b@c:d", "@host:p", "a@:path", "a@@b:c", "@:p",

	// Degenerate components: an empty host, an empty path, or both.
	":", "a@:", "[a]:", "@:",

	// Host boundaries, and the bytes that used to be excluded.
	"host:path", ":path", "host:", "ho st:path", "ho\tst:path",
	"ho\nst:path", "ho\rst:path", "ho\fst:path", "ho\vst:path",
	"ho\x00st:path", "ho\xffst:path", "h\xc3\xa9st:path",

	// Colons past the first one, which are all path, never a port.
	"h:22:p", "h:0:p", "h:99999:p", "h:123456:p", "h:22:", "h:22:\\p",
	"h:007/bond", "h::p", "h:2a:p",

	// Bracketed hosts and the bare-host fallback.
	"[fe80::1]:repo.git", "git@[fe80::1]:repo.git", "[fe80::1]:22:repo.git",
	"[a:b]:c", "[a:b]:\\c", "[a]:c", "[a]:", "[]:p", "[:p", "[a:c",
	"[a]x:c", "[a b]:c", "[a]::c", "a@[b]:c", "[a@b]:c", "[[a]:c",
	"[x@y]:a@[b", "a@b@[c]:d", "user@[a@b]:repo", "host:path@other:repo",
	"[myhost:123]:src", "user@[myhost:123]:src", "[fe80:1]:repo",

	// Path rules.
	"h:\\p", "h:p\nq", "h:p\n", "h:\np", "h:\n", "h:\\", "h:p\\q",
	"h:\x00", "h:\xc3\xa9\n", "[a]b",
	"[a:b]",

	// Real-world shapes.
	"git@github.com:james/bond", "git@github.com:22:james/bond",
	"git@github.com:22:007/bond", "git@github.com:_james/bond.git",
	"git@[fe80::1]:james/bond",
	"user@host.example.com:path/to/repo.git",
	"/abs/path/with:colon/file", "./relative:path", "sub/dir:foo",
	"C:foo", "C:/path/to/repo", "C:\\path\\to\\repo", "d:relative",
	"/foo.git", "foo.git", "file:///foo.git", "file://C:/path/to/repo",
}

// Component expectations were checked against Git's fetch-pack --diag-url.
// Brackets remain at this layer; ParseSCP unwraps them and extracts ports.
func TestScannerRules(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		input, user, host, path string
	}{
		{"a@b:c", "a", "b", "c"},
		{"a@b@c:d", "a@b", "c", "d"},
		{"a@@b:c", "a@", "b", "c"},
		{"@host:p", "", "host", "p"},
		{"@:p", "", "", "p"},
		{"a@:path", "a", "", "path"},
		{"a@:", "a", "", ""},
		{"host:path@other:repo", "", "host", "path@other:repo"},
		{"host:path", "", "host", "path"},
		{"ho st:path", "", "ho st", "path"},
		{"ho\tst:path", "", "ho\tst", "path"},
		{"ho\nst:path", "", "ho\nst", "path"},
		{"ho\rst:path", "", "ho\rst", "path"},
		{"ho\fst:path", "", "ho\fst", "path"},
		{"ho\vst:path", "", "ho\vst", "path"},
		{"ho\x00st:path", "", "ho\x00st", "path"},
		{"ho\xffst:path", "", "ho\xffst", "path"},
		{"h:22:p", "", "h", "22:p"},
		{"h:0:p", "", "h", "0:p"},
		{"h:99999:p", "", "h", "99999:p"},
		{"h:123456:p", "", "h", "123456:p"},
		{"h:007/bond", "", "h", "007/bond"},
		{"h:2a:p", "", "h", "2a:p"},
		{"h::p", "", "h", ":p"},
		{"h:22:", "", "h", "22:"},
		{"h:22:\\p", "", "h", "22:\\p"},
		{"[fe80::1]:repo.git", "", "[fe80::1]", "repo.git"},
		{"git@[fe80::1]:repo.git", "git", "[fe80::1]", "repo.git"},
		{"[fe80::1]:22:repo.git", "", "[fe80::1]", "22:repo.git"},
		{"[myhost:123]:src", "", "[myhost:123]", "src"},
		{"user@[myhost:123]:src", "user", "[myhost:123]", "src"},
		{"[a:b]:c", "", "[a:b]", "c"},
		{"[a]:c", "", "[a]", "c"},
		{"[a]::c", "", "[a]", ":c"},
		{"[a]:", "", "[a]", ""},
		{"[a b]:c", "", "[a b]", "c"},
		{"[a:b]:\\c", "", "[a:b]", "\\c"},
		{"[]:p", "", "[]", "p"},
		{"[a:c", "", "[a", "c"},
		{"[:p", "", "[", "p"},
		{"[a]x:c", "", "[a]", "c"},
		{"[[a]:c", "", "[[a]", "c"},
		{"a@[b]:c", "a", "[b]", "c"},
		{"[a@b]:c", "", "[a@b]", "c"},
		{"[x@y]:a@[b", "", "[x@y]", "a@[b"},
		{"a@b@[c]:d", "a@b", "[c]", "d"},
		{"user@[a@b]:c", "user", "[a@b]", "c"},
		{"host:", "", "host", ""},
		{":path", "", "", "path"},
		{":", "", "", ""},
		{"h:\\p", "", "h", "\\p"},
		{"h:\\", "", "h", "\\"},
		{"h:p\\q", "", "h", "p\\q"},
		{"h:p\nq", "", "h", "p\nq"},
		{"h:p\n", "", "h", "p\n"},
		{"h:\np", "", "h", "\np"},
		{"h:\n", "", "h", "\n"},
		{"h:\xc3\xa9p", "", "h", "\xc3\xa9p"},
		{"h:\xc3\xa9\n", "", "h", "\xc3\xa9\n"},
		{"h:\x00", "", "h", "\x00"},
		{"git@github.com:james/bond", "git", "github.com", "james/bond"},
		{"C:\\foo", "", "C", "\\foo"},
		{"C:/path/to/repo", "", "C", "/path/to/repo"},
		{"./rel:path", "", "./rel", "path"},
		{"/abs/path/with:colon/file", "", "/abs/path/with", "colon/file"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			checkEquiv(t, tc.input)
			user, host, path, ok := matchScpLike(tc.input)
			if !ok || user != tc.user || host != tc.host || path != tc.path {
				t.Errorf("matchScpLike(%q) = (%q, %q, %q, %v), want (%q, %q, %q, true)",
					tc.input, user, host, path, ok, tc.user, tc.host, tc.path)
			}
		})
	}
}

func TestSchemeRules(t *testing.T) {
	t.Parallel()
	t.Log("`://` is all there is to it: Git's parse_connect_url does " +
		"strstr(url, \"://\") and calls everything before the FIRST one the " +
		"scheme, so the separator need not follow the first `:`, the scheme " +
		"need not be syntactically valid, and it need not be non-empty. An " +
		"endpoint Git cannot name a protocol for is refused, never re-read " +
		"as an SCP-like or local one.")

	for _, c := range []struct {
		in   string
		want bool
	}{
		{"a://b", true},
		{"http://host/path", true},
		{"", false},
		{":", false},
		{"a:/b", false}, // one slash
		{"a:", false},   // nothing after the colon
		// An empty scheme is still a scheme: Git dies with
		// "protocol '' is not supported".
		{"://a", true},
		{"://", true},
		// The separator is the first `://` anywhere, not one opening at
		// the first `:`. Git dies with "protocol 'a:b' is not supported".
		{"a:b://c", true},
		// Which reaches endpoints that otherwise read as SCP-like or
		// local. Git: "protocol 'git@host:a'", "protocol '/abs/a'".
		{"git@host:a://b", true},
		{"/abs/a://b", true},
		{"./foo://bar", true},
		{"a\n://b", true}, // no byte is excluded from the scheme
		{"\x00://b", true},
		{"\xff://b", true},
	} {
		checkEquiv(t, c.in)
		if got := MatchesScheme(c.in); got != c.want {
			t.Errorf("MatchesScheme(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMatchesScpLikeLayering(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		in             string
		grammar, final bool
	}{
		{"git@github.com:james/bond", true, true},
		{"host:path", true, true},
		{"[fe80::1]:repo.git", true, true},
		{"git@[fe80::1]:repo.git", true, true},
		// Grammar matches; the `/`-before-`:` rule rejects it.
		{"./rel:path", true, false},
		{"/abs/path/with:colon/file", true, false},
		{"sub/dir:foo", true, false},
		// The degenerate halves are SSH, exactly as in Git.
		{"host:", true, true},
		{":path", true, true},
		{":", true, true},
		// A DOS path matches the grammar everywhere. It is rejected a
		// layer up on Windows only, by hasDosDrivePrefix -- and Git
		// does exactly the same, because has_dos_drive_prefix is a
		// no-op off Windows (git-compat-util.h). git 2.55 on this
		// machine asks HOST=[C] for CMD=[git-upload-pack '\\foo'].
		{"C:\\foo", true, runtime.GOOS != "windows"},
		{"C:/foo", true, runtime.GOOS != "windows"},
		// Grammar rejects it outright: no colon at all.
		{"a@b", false, false},
	} {
		_, _, _, grammar := matchScpLike(c.in)
		if grammar != c.grammar {
			t.Errorf("matchScpLike(%q) ok = %v, want %v", c.in, grammar, c.grammar)
		}
		if final := MatchesScpLike(c.in); final != c.final {
			t.Errorf("MatchesScpLike(%q) = %v, want %v", c.in, final, c.final)
		}
	}
}

func TestScannerMatchesRegexpBytePositions(t *testing.T) {
	t.Parallel()

	slots := []struct{ prefix, suffix string }{
		{"", ""},
		{"", "host:path"},
		{"us", "er@host:path"},
		{"user", "@host:path"},
		{"user@", "host:path"},
		{"ho", "st:path"},
		{"host", ":path"},
		{"host:", "path"},
		{"host:", "2:path"},
		{"host:2", "2:path"},
		{"host:22", ":path"},
		{"host:22:", "path"},
		{"host:22:pa", "th"},
		{"host:22:path", ""},
		{"host:9999", "9:path"},
		{"host:12345", "6:path"},
		{"host:99999", ":path"},
		{"", "://host"},
		{"ht", "tp://host"},
		{"http", "//host"},
		{"http:", "/host"},
		{"a@b", "@c:d"},
		{"h:", "\\path"},
		{"h:p", "q"},
		{"", "[fe80::1]:path"},
		{"[", "fe80::1]:path"},
		{"[fe80", "::1]:path"},
		{"[fe80::1", "]:path"},
		{"[fe80::1]", ":path"},
		{"[fe80::1]:", "path"},
		{"user@", "[fe80::1]:path"},
		{"user@[fe80::1]", ":path"},
		{"[a", "]:p"},
		{"[a]", ":p"},
		{"[a]:", "p"},
		{"[a]:p", ""},
		{"[", "]:p"},
	}

	n := 0
	for _, slot := range slots {
		for b := range 256 {
			checkEquiv(t, slot.prefix+string([]byte{byte(b)})+slot.suffix)
			n++
		}
	}
	t.Logf("checked %d strings (%d byte values x %d positions)", n, 256, len(slots))
}

// TestScannerMatchesRegexpExhaustiveBytes includes valid and invalid UTF-8
// sequences to check that the byte scanner agrees with the regexp engine,
// which matches runes.
func TestScannerMatchesRegexpExhaustiveBytes(t *testing.T) {
	t.Parallel()

	alphabet := []byte{
		':', '@', '/', '\\', '1', 'a', '[', ']',
		' ', '\n', '\v', 0x00, 0xc3, 0xa9,
	}
	depth := 5
	if wide() {
		depth = 7
	}

	n := 0
	var rec func(prefix []byte, left int)
	rec = func(prefix []byte, left int) {
		checkEquiv(t, string(prefix))
		n++
		if left == 0 {
			return
		}
		for _, c := range alphabet {
			rec(append(prefix, c), left-1)
		}
	}
	rec(make([]byte, 0, depth), depth)

	t.Logf("checked %d strings (all lengths 0..%d over %d bytes)", n, depth, len(alphabet))
}

// TestScannerMatchesRegexpExhaustiveTokens uses tokens to cover long digit
// runs and bracketed hosts without enumerating every intervening byte string.
func TestScannerMatchesRegexpExhaustiveTokens(t *testing.T) {
	t.Parallel()

	tokens := []string{
		"a", "@", ":", "/", "\\", "0", "22", "99999", "123456",
		"\n", "\v", "\t", " ", "\x00", "\xff", "\xc3\xa9",
		"a@", "h:", ":p", "[", "]", "[a]", "]:", "[fe80::1]",
	}
	depth := 3
	if wide() {
		depth = 5
	}

	n := 0
	var rec func(prefix string, left int)
	rec = func(prefix string, left int) {
		checkEquiv(t, prefix)
		n++
		if left == 0 {
			return
		}
		for _, tok := range tokens {
			rec(prefix+tok, left-1)
		}
	}
	rec("", depth)

	// Guard the shapes this sweep exists to reach.
	for _, s := range []string{
		"h:99999:p", "h:123456:p", "a@h:22:p",
		"[fe80::1]:p", "a@[fe80::1]:p", "[fe80::1]:22:p",
	} {
		checkEquiv(t, s)
	}
	t.Logf("checked %d strings (all lengths 0..%d over %d tokens)", n, depth, len(tokens))
}

func TestScannerMatchesRegexpRandom(t *testing.T) {
	t.Parallel()

	tokens := []string{
		":", "@", "/", "\\", "0", "9", "a", "Z", " ", "\n", "\t", "\r", "\f", "\v",
		"\x00", "\xff", "\xc3\xa9", "\xe2\x82\xac", "\xf0\x9f\x98\x80",
		".git", "22", "99999", "123456", "1234567890",
		"[", "]", "[a]", "[fe80::1]", "]:",
	}
	rounds := 50000
	if wide() {
		rounds = 20000000
	}

	rng := rand.New(rand.NewSource(1))
	var b []byte
	for i := 0; i < rounds; i++ {
		b = b[:0]
		for n := rng.Intn(20); n >= 0; n-- {
			b = append(b, tokens[rng.Intn(len(tokens))]...)
		}
		checkEquiv(t, string(b))
	}
	t.Logf("checked %d random strings", rounds)
}

func TestScannerMatchesRegexpSeeds(t *testing.T) {
	t.Parallel()

	for _, s := range equivSeeds {
		checkEquiv(t, s)
	}
	t.Logf("checked %d seeds", len(equivSeeds))
}

func TestFindScpLikeComponentsOnNonMatch(t *testing.T) {
	t.Parallel()

	// A bracketed host requires a colon after its closing bracket.
	for _, s := range []string{
		"",     // nothing at all
		"host", // no colon
		"a@b",  // user branch and no-user branch both lack a colon
		"[a]b", // a bracketed host with no `:` to close it
		"\xff", // an arbitrary byte string with no colon in it
	} {
		if oracleSCPComponents(s) != nil {
			t.Fatalf("%q was meant to be a non-match", s)
		}

		user, host, path, ok := FindScpLikeComponents(s)
		if ok {
			t.Errorf("FindScpLikeComponents(%q) reported a match", s)
		}
		if user != "" || host != "" || path != "" {
			t.Errorf("FindScpLikeComponents(%q) = (%q, %q, %q), want all empty",
				s, user, host, path)
		}
	}
}
