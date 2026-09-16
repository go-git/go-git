package url

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"testing"
)

// The oracle expressions specify the grammars used by the URL scanners.
// The differential tests compare match results and extracted components.
var (
	oracleScheme = regexp.MustCompile(`://`)
	oracleScp    = regexp.MustCompile(`^(?:(?P<user>[^@]+)@)?(?P<host>\[[^\]\s]+\]|[^:\s]*):(?P<path>(?:[^\\].*)?)$`)
)

// equivDiff returns a description of the first difference between the
// scanners and the oracle expressions, or an empty string if they agree.
func equivDiff(s string) string {
	if got, want := MatchesScheme(s), oracleScheme.MatchString(s); got != want {
		return fmt.Sprintf("MatchesScheme(%q) = %v, regexp says %v", s, got, want)
	}

	user, host, path, ok := matchScpLike(s)
	m := oracleScp.FindStringSubmatch(s)
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

	// Host boundaries and the whitespace class.
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

	// Path rules.
	"h:\\p", "h:p\nq", "h:p\n", "h:\np", "h:\n", "h:\\", "h:p\\q",

	// Real-world shapes.
	"git@github.com:james/bond", "git@github.com:22:james/bond",
	"git@github.com:22:007/bond", "git@github.com:_james/bond.git",
	"git@[fe80::1]:james/bond",
	"user@host.example.com:path/to/repo.git",
	"/abs/path/with:colon/file", "./relative:path", "sub/dir:foo",
	"C:foo", "C:/path/to/repo", "C:\\path\\to\\repo", "d:relative",
	"/foo.git", "foo.git", "file:///foo.git", "file://C:/path/to/repo",
}

func TestScannerRules(t *testing.T) {
	t.Parallel()

	type want struct {
		ok               bool
		user, host, path string
	}
	type kase struct {
		in string
		want
	}

	for _, rule := range []struct {
		name  string
		why   string
		cases []kase
	}{
		{
			name: "UserCannotCrossFirstAt",
			why: "`[^@]+` excludes `@` and is closed by a literal `@`, so the user " +
				"group can only ever be the text before the FIRST `@`, and must be " +
				"non-empty. The scanner therefore needs one IndexByte, not a search.",
			cases: []kase{
				{"a@b:c", want{true, "a", "b", "c"}},
				// Later `@`s fall inside the host, which permits them.
				{"a@b@c:d", want{true, "a", "b@c", "d"}},
				// at == 0 leaves the user group empty, so the branch cannot be taken.
				{"@host:p", want{true, "", "@host", "p"}},
				{"@:p", want{true, "", "@", "p"}},
				// Consecutive `@`: user is "a", host is "@b".
				{"a@@b:c", want{true, "a", "@b", "c"}},
			},
		},
		{
			name: "UserBranchIsGreedyButFallsBack",
			why: "`(?:...)?` is greedy, so a parse that fills the user group outranks " +
				"one that does not; when the rest of the grammar cannot be satisfied " +
				"with a user, the no-user parse wins instead. The scanner mirrors that " +
				"with an explicit second attempt.",
			cases: []kase{
				// Both branches match. Greedy wins: user=a, not host="a@b".
				{"a@b:c", want{true, "a", "b", "c"}},
				// The host may be empty, so the user branch now succeeds
				// here and the greedy parse wins. Git splits the same
				// endpoint into the single host `a@`, which ssh then
				// reads as user `a` and an empty host, so the two agree
				// on what is dialled and differ only in which component
				// the `a` is reported in.
				{"a@:path", want{true, "a", "", "path"}},
				{"a@:", want{true, "a", "", ""}},
				// User branch fails (rest has no `:`), so no-user is
				// used -- and it has no `:` either, so nothing matches.
				{"a@b", want{false, "", "", ""}},
			},
		},
		{
			name: "HostCannotCrossFirstColonAndIsWhitespaceFree",
			why: "`[^:\\s]*` excludes `:` and is closed by a literal `:`, so the host " +
				"is exactly the text before the FIRST `:`. RE2's `\\s` is " +
				"[\\t\\n\\f\\r ] — it does NOT include `\\v`, so a vertical tab is a " +
				"legal host byte. Shortening the host cannot rescue a match, because " +
				"the next byte would then not be the `:` the grammar demands.",
			cases: []kase{
				{"host:path", want{true, "", "host", "path"}},
				// Each member of RE2's \s class rejects the host.
				{"ho st:path", want{false, "", "", ""}},
				{"ho\tst:path", want{false, "", "", ""}},
				{"ho\nst:path", want{false, "", "", ""}},
				{"ho\rst:path", want{false, "", "", ""}},
				{"ho\fst:path", want{false, "", "", ""}},
				// \v is NOT in \s, so this one matches.
				{"ho\vst:path", want{true, "", "ho\vst", "path"}},
				// NUL and invalid UTF-8 are ordinary host bytes.
				{"ho\x00st:path", want{true, "", "ho\x00st", "path"}},
				{"ho\xffst:path", want{true, "", "ho\xffst", "path"}},
			},
		},
		{
			name: "NoPortAfterTheHost",
			why: "The SCP-like form has no port. Git splits it in parse_connect_url " +
				"(connect.c) at the FIRST `:` at or after the host, and everything " +
				"past that `:` is the path; get_host_and_port, which is where a port " +
				"could come from, only ever runs on the host half, and in this form " +
				"that half holds no `:`. go-git used to read a leading 1..5 digit " +
				"run closed by a second `:` as a port, so `host:22:007/bond` asked " +
				"for `007/bond` on TCP 22 where Git asks for `22:007/bond` on the " +
				"default port.",
			cases: []kase{
				// Every one of these used to report a port.
				{"h:22:p", want{true, "", "h", "22:p"}},
				{"h:0:p", want{true, "", "h", "0:p"}},
				{"h:99999:p", want{true, "", "h", "99999:p"}},
				{"git@host:22:007/bond", want{true, "git", "host", "22:007/bond"}},
				// These never did, and are unchanged.
				{"h:123456:p", want{true, "", "h", "123456:p"}},
				{"h:007/bond", want{true, "", "h", "007/bond"}},
				{"h:2a:p", want{true, "", "h", "2a:p"}},
				{"h::p", want{true, "", "h", ":p"}},
				// The port branch used to be abandoned here, for an empty path
				// and for a path opening with a backslash. There is only ever
				// the one parse now.
				{"h:22:", want{true, "", "h", "22:"}},
				{"h:22:\\p", want{true, "", "h", "22:\\p"}},
			},
		},
		{
			name: "BracketedHostIsTheFirstAlternative",
			why: "`\\[[^\\]\\s]+\\]` stands in for Git's host_end (connect.c): a host " +
				"written as a bracketed literal, which is how an IPv6 address has to " +
				"be written, ends at the `]` and not at a `:` inside it. The body " +
				"cannot hold a `]`, so the literal ends at the FIRST one; it must be " +
				"non-empty, and — as everywhere else in this grammar, which is " +
				"stricter than Git about whitespace — hold none. The brackets stay " +
				"in the returned host: that keeps it a substring of the endpoint, it " +
				"is the form net/url wants in URL.Host, and it is what has to be " +
				"written back out for the URL to parse the same way again.",
			cases: []kase{
				{"[fe80::1]:repo.git", want{true, "", "[fe80::1]", "repo.git"}},
				{"git@[fe80::1]:repo.git", want{true, "git", "[fe80::1]", "repo.git"}},
				// No port here either: Git asks for the path `22:repo.git`.
				{"[fe80::1]:22:repo.git", want{true, "", "[fe80::1]", "22:repo.git"}},
				// The alternative is FIRST, so it beats the bare host `[a`.
				{"[a:b]:c", want{true, "", "[a:b]", "c"}},
				{"[a]:c", want{true, "", "[a]", "c"}},
				{"[a]::c", want{true, "", "[a]", ":c"}},
				// The path may be empty, so the literal stands on its own.
				{"[a]:", want{true, "", "[a]", ""}},
				// Whitespace in the body rules the literal out, and the bare
				// host left over holds whitespace too.
				{"[a b]:c", want{false, "", "", ""}},
			},
		},
		{
			name: "BracketedHostFallsBackToTheBareHost",
			why: "The alternation is ordered, not committed: when the bracketed " +
				"parse cannot be completed the bare-host parse is tried on the same " +
				"input, exactly as the regexp backtracks, and the scanner makes that " +
				"second attempt explicitly. Note where this leaves go-git stricter " +
				"than Git: Git accepts junk between the `]` and the `:` and silently " +
				"drops it (`[a]b:c` reaches host `a`), which would make the returned " +
				"host text the user never wrote, so go-git does not mirror it and " +
				"such an endpoint keeps its bare-host reading. Git also looks for " +
				"the first `@[` rather than the first `@`, so `[a@b]:c` and " +
				"`a@b@[c]:d` split differently there; no address is written either " +
				"way.",
			cases: []kase{
				// Empty body, so the literal cannot match; the bare host can.
				{"[]:p", want{true, "", "[]", "p"}},
				// Unterminated bracket. Git falls back here too.
				{"[a:c", want{true, "", "[a", "c"}},
				{"[:p", want{true, "", "[", "p"}},
				// `]` not closed by the `:`. Git would reach host `a`.
				{"[a]x:c", want{true, "", "[a]x", "c"}},
				// The literal parses but its path does not, so the bare host
				// wins and reads the `]` as part of the path.
				{"[a:b]:\\c", want{true, "", "[a", "b]:\\c"}},
				// A `[` inside the body is an ordinary byte.
				{"[[a]:c", want{true, "", "[[a]", "c"}},
				// The user is split at the first `@`, before the host
				// alternation is reached at all.
				{"a@[b]:c", want{true, "a", "[b]", "c"}},
				{"[a@b]:c", want{true, "[a", "b]", "c"}},
			},
		},
		{
			name: "DegenerateComponentsAreLegal",
			why: "The colon is the whole of the SCP-like syntax: Git's " +
				"url_is_local_not_ssh (url.c) asks only whether there IS a colon " +
				"with no `/` before it, and parse_connect_url then splits on it " +
				"with no minimum length on either half. So `host:` asks the host " +
				"for `git-upload-pack ''` and `:path` asks the empty host — which " +
				"ssh reads as the local machine — for `path`. go-git used to " +
				"require both halves to be non-empty and so read both as LOCAL " +
				"PATHS, opening a repository at the literal text instead.",
			cases: []kase{
				// git 2.55: HOST=[host] CMD=[git-upload-pack '']
				{"host:", want{true, "", "host", ""}},
				// git 2.55: HOST=[] CMD=[git-upload-pack 'path']
				{":path", want{true, "", "", "path"}},
				// git 2.55: HOST=[] CMD=[git-upload-pack '']
				{":", want{true, "", "", ""}},
				{"[a]:", want{true, "", "[a]", ""}},
				// git 2.55: HOST=[a@] CMD=[git-upload-pack 'path'] -- the
				// same dial, reported with the `a` in the user group.
				{"a@:path", want{true, "a", "", "path"}},
				{"a@:", want{true, "a", "", ""}},
				{"@:p", want{true, "", "@", "p"}},
				// A colon is still required: no colon, no match.
				{"host", want{false, "", "", ""}},
				{"", want{false, "", "", ""}},
				{"a@b", want{false, "", "", ""}},
				// The empty path does not rescue a path the grammar
				// rejects; `$` still has to be reached.
				{"h:\\p", want{false, "", "", ""}},
				{"h:p\n", want{false, "", "", ""}},
			},
		},
		{
			name: "PathRules",
			why: "`(?:[^\\\\].*)?$` means: empty, or a first rune that is not a " +
				"backslash followed by anything `.` matches — and `.` excludes " +
				"`\\n` while `$` is end-of-text (no (?m)), so no newline may appear " +
				"past that first rune, a TRAILING newline included. Note " +
				"`[^\\\\]` does match `\\n`, so a newline is legal as the first rune. " +
				"Scanning for `\\n` from byte 1 is exact because `\\n` is never a " +
				"UTF-8 continuation byte.",
			cases: []kase{
				{"h:p", want{true, "", "h", "p"}},
				// Leading backslash.
				{"h:\\p", want{false, "", "", ""}},
				{"h:\\", want{false, "", "", ""}},
				// A backslash anywhere else is fine.
				{"h:p\\q", want{true, "", "h", "p\\q"}},
				// Newline past the first rune, including at the very end.
				{"h:p\nq", want{false, "", "", ""}},
				{"h:p\n", want{false, "", "", ""}},
				// Newline AS the first rune is accepted by `[^\\]`.
				{"h:\np", want{true, "", "h", "\np"}},
				{"h:\n", want{true, "", "h", "\n"}},
				// Multi-byte first rune, then the \n scan starts mid-rune yet is exact.
				{"h:\xc3\xa9p", want{true, "", "h", "\xc3\xa9p"}},
				{"h:\xc3\xa9\n", want{false, "", "", ""}},
			},
		},
		{
			name: "RealWorldShapes",
			why: "The forms the rest of the suite and the transport tests rely on. " +
				"Note that matchScpLike is only the grammar: `./rel:path` and `C:/foo` " +
				"match it and are rejected a layer up, by MatchesScpLike.",
			cases: []kase{
				{"git@github.com:james/bond", want{true, "git", "github.com", "james/bond"}},
				{"git@host:22:007/bond", want{true, "git", "host", "22:007/bond"}},
				{"git@[fe80::1]:james/bond", want{true, "git", "[fe80::1]", "james/bond"}},
				{"git@github.com:_james/bond.git", want{true, "git", "github.com", "_james/bond.git"}},
				{"user@host.example.com:path/to/repo.git", want{true, "user", "host.example.com", "path/to/repo.git"}},
				{"host:path", want{true, "", "host", "path"}},
				// A DOS path with backslashes fails the grammar outright.
				{"C:\\foo", want{false, "", "", ""}},
				// A DOS path with forward slashes does NOT; MatchesScpLike rejects it.
				{"C:/path/to/repo", want{true, "", "C", "/path/to/repo"}},
				// Local paths match the grammar; MatchesScpLike rejects them.
				{"./rel:path", want{true, "", "./rel", "path"}},
				{"/abs/path/with:colon/file", want{true, "", "/abs/path/with", "colon/file"}},
				{"@host:p", want{true, "", "@host", "p"}},
				{"a@b@host:p", want{true, "a", "b@host", "p"}},
				{"host:", want{true, "", "host", ""}},
				{":path", want{true, "", "", "path"}},
			},
		},
	} {
		t.Run(rule.name, func(t *testing.T) {
			t.Parallel()
			t.Log(rule.why)

			for _, c := range rule.cases {
				checkEquiv(t, c.in)

				user, host, path, ok := matchScpLike(c.in)
				if ok != c.ok {
					t.Errorf("matchScpLike(%q) ok = %v, want %v", c.in, ok, c.ok)
					continue
				}
				if !ok {
					continue
				}
				if user != c.user || host != c.host || path != c.path {
					t.Errorf("matchScpLike(%q) = (user=%q host=%q path=%q), want (user=%q host=%q path=%q)",
						c.in, user, host, path, c.user, c.host, c.path)
				}
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
		// Grammar rejects it outright.
		{"C:\\foo", false, false},
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

	for _, s := range []string{
		"",           // nothing at all
		"host",       // no colon
		"C:\\foo",    // path opens with a backslash
		"h:p\n",      // trailing newline
		"ho st:path", // whitespace in the host
		"h:\\",       // path is a lone backslash
		"a@b",        // user branch and no-user branch both lack a colon
		"[a b]:c",    // whitespace survives both host alternatives
	} {
		if oracleScp.FindStringSubmatch(s) != nil {
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
