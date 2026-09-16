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
	oracleScheme = regexp.MustCompile(`^[^:]+://`)
	oracleScp    = regexp.MustCompile(`^(?:(?P<user>[^@]+)@)?(?P<host>[^:\s]+):(?:(?P<port>[0-9]{1,5}):)?(?P<path>[^\\].*)$`)
)

// equivDiff returns a description of the first difference between the
// scanners and the oracle expressions, or an empty string if they agree.
func equivDiff(s string) string {
	if got, want := MatchesScheme(s), oracleScheme.MatchString(s); got != want {
		return fmt.Sprintf("MatchesScheme(%q) = %v, regexp says %v", s, got, want)
	}

	user, host, port, path, ok := matchScpLike(s)
	m := oracleScp.FindStringSubmatch(s)
	if ok != (m != nil) {
		return fmt.Sprintf("matchScpLike(%q) ok = %v, regexp says %v", s, ok, m != nil)
	}
	if !ok {
		return ""
	}
	if user != m[1] || host != m[2] || port != m[3] || path != m[4] {
		return fmt.Sprintf("matchScpLike(%q) = (user=%q host=%q port=%q path=%q), regexp = (user=%q host=%q port=%q path=%q)",
			s, user, host, port, path, m[1], m[2], m[3], m[4])
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
	"ssh://git@github.com/user/repository.git",
	"http://git:pass@github.com:8080/user/repository.git?foo#bar",

	// User group boundaries.
	"a@b:c", "a@b@c:d", "@host:p", "a@:path", "a@@b:c", "@:p",

	// Host boundaries and the whitespace class.
	"host:path", ":path", "host:", "ho st:path", "ho\tst:path",
	"ho\nst:path", "ho\rst:path", "ho\fst:path", "ho\vst:path",
	"ho\x00st:path", "ho\xffst:path", "h\xc3\xa9st:path",

	// Port run and its fallback.
	"h:22:p", "h:0:p", "h:99999:p", "h:123456:p", "h:22:", "h:22:\\p",
	"h:007/bond", "h::p", "h:2a:p",

	// Path rules.
	"h:\\p", "h:p\nq", "h:p\n", "h:\np", "h:\n", "h:\\", "h:p\\q",

	// Real-world shapes.
	"git@github.com:james/bond", "git@github.com:22:james/bond",
	"git@github.com:22:007/bond", "git@github.com:_james/bond.git",
	"user@host.example.com:path/to/repo.git",
	"/abs/path/with:colon/file", "./relative:path", "sub/dir:foo",
	"C:foo", "C:/path/to/repo", "C:\\path\\to\\repo", "d:relative",
	"/foo.git", "foo.git", "file:///foo.git", "file://C:/path/to/repo",
}

func TestScannerRules(t *testing.T) {
	t.Parallel()

	type want struct {
		ok                     bool
		user, host, port, path string
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
				{"a@b:c", want{true, "a", "b", "", "c"}},
				// Later `@`s fall inside the host, which permits them.
				{"a@b@c:d", want{true, "a", "b@c", "", "d"}},
				// at == 0 leaves the user group empty, so the branch cannot be taken.
				{"@host:p", want{true, "", "@host", "", "p"}},
				{"@:p", want{true, "", "@", "", "p"}},
				// Consecutive `@`: user is "a", host is "@b".
				{"a@@b:c", want{true, "a", "@b", "", "c"}},
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
				{"a@b:c", want{true, "a", "b", "", "c"}},
				// User branch fails (rest begins with `:`), so no-user is used.
				{"a@:path", want{true, "", "a@", "", "path"}},
				// User branch fails (rest has no `:`), so no-user is used.
				{"a@b", want{false, "", "", "", ""}},
			},
		},
		{
			name: "HostCannotCrossFirstColonAndIsWhitespaceFree",
			why: "`[^:\\s]+` excludes `:` and is closed by a literal `:`, so the host " +
				"is exactly the text before the FIRST `:`, non-empty. RE2's `\\s` is " +
				"[\\t\\n\\f\\r ] — it does NOT include `\\v`, so a vertical tab is a " +
				"legal host byte. Shortening the host cannot rescue a match, because " +
				"the next byte would then not be the `:` the grammar demands.",
			cases: []kase{
				{"host:path", want{true, "", "host", "", "path"}},
				// Empty host.
				{":path", want{false, "", "", "", ""}},
				// Each member of RE2's \s class rejects the host.
				{"ho st:path", want{false, "", "", "", ""}},
				{"ho\tst:path", want{false, "", "", "", ""}},
				{"ho\nst:path", want{false, "", "", "", ""}},
				{"ho\rst:path", want{false, "", "", "", ""}},
				{"ho\fst:path", want{false, "", "", "", ""}},
				// \v is NOT in \s, so this one matches.
				{"ho\vst:path", want{true, "", "ho\vst", "", "path"}},
				// NUL and invalid UTF-8 are ordinary host bytes.
				{"ho\x00st:path", want{true, "", "ho\x00st", "", "path"}},
				{"ho\xffst:path", want{true, "", "ho\xffst", "", "path"}},
			},
		},
		{
			name: "PortIsTheFullLeadingDigitRun",
			why: "`[0-9]{1,5}` is greedy and is closed by a literal `:`. A shorter " +
				"run would leave a digit where the `:` must be, so the only candidate " +
				"is the WHOLE leading-digit run, and it matches only when that run is " +
				"1..5 long and the next byte is `:`.",
			cases: []kase{
				{"h:22:p", want{true, "", "h", "22", "p"}},
				{"h:0:p", want{true, "", "h", "0", "p"}},
				// Five digits is the upper bound.
				{"h:99999:p", want{true, "", "h", "99999", "p"}},
				// Six is one too many: no port, the digits become the path.
				{"h:123456:p", want{true, "", "h", "", "123456:p"}},
				// Run not closed by `:`.
				{"h:007/bond", want{true, "", "h", "", "007/bond"}},
				{"h:2a:p", want{true, "", "h", "", "2a:p"}},
				// Empty run.
				{"h::p", want{true, "", "h", "", ":p"}},
			},
		},
		{
			name: "PortBranchIsGreedyButFallsBack",
			why: "The port group is greedy too, so a parse with a port outranks one " +
				"without; when the path that follows cannot be satisfied, the parse " +
				"that reads the digits as part of the path wins. The scanner makes the " +
				"same second attempt.",
			cases: []kase{
				// Port branch leaves an empty path, so it is abandoned.
				{"h:22:", want{true, "", "h", "", "22:"}},
				// Port branch leaves a path starting with `\`, so it is abandoned.
				{"h:22:\\p", want{true, "", "h", "", "22:\\p"}},
				// Port branch succeeds, and is preferred over path="22:p".
				{"h:22:p", want{true, "", "h", "22", "p"}},
			},
		},
		{
			name: "PathRules",
			why: "`[^\\\\].*$` means: non-empty, first rune is not a backslash, and " +
				"`.` excludes `\\n` while `$` is end-of-text (no (?m)), so no newline " +
				"may appear past that first rune — a TRAILING newline fails too. Note " +
				"`[^\\\\]` does match `\\n`, so a newline is legal as the first rune. " +
				"Scanning for `\\n` from byte 1 is exact because `\\n` is never a " +
				"UTF-8 continuation byte.",
			cases: []kase{
				{"h:p", want{true, "", "h", "", "p"}},
				// Empty path.
				{"host:", want{false, "", "", "", ""}},
				// Leading backslash.
				{"h:\\p", want{false, "", "", "", ""}},
				{"h:\\", want{false, "", "", "", ""}},
				// A backslash anywhere else is fine.
				{"h:p\\q", want{true, "", "h", "", "p\\q"}},
				// Newline past the first rune, including at the very end.
				{"h:p\nq", want{false, "", "", "", ""}},
				{"h:p\n", want{false, "", "", "", ""}},
				// Newline AS the first rune is accepted by `[^\\]`.
				{"h:\np", want{true, "", "h", "", "\np"}},
				{"h:\n", want{true, "", "h", "", "\n"}},
				// Multi-byte first rune, then the \n scan starts mid-rune yet is exact.
				{"h:\xc3\xa9p", want{true, "", "h", "", "\xc3\xa9p"}},
				{"h:\xc3\xa9\n", want{false, "", "", "", ""}},
			},
		},
		{
			name: "RealWorldShapes",
			why: "The forms the rest of the suite and the transport tests rely on. " +
				"Note that matchScpLike is only the grammar: `./rel:path` and `C:/foo` " +
				"match it and are rejected a layer up, by MatchesScpLike.",
			cases: []kase{
				{"git@github.com:james/bond", want{true, "git", "github.com", "", "james/bond"}},
				{"git@host:22:007/bond", want{true, "git", "host", "22", "007/bond"}},
				{"git@github.com:_james/bond.git", want{true, "git", "github.com", "", "_james/bond.git"}},
				{"user@host.example.com:path/to/repo.git", want{true, "user", "host.example.com", "", "path/to/repo.git"}},
				{"host:path", want{true, "", "host", "", "path"}},
				// A DOS path with backslashes fails the grammar outright.
				{"C:\\foo", want{false, "", "", "", ""}},
				// A DOS path with forward slashes does NOT; MatchesScpLike rejects it.
				{"C:/path/to/repo", want{true, "", "C", "", "/path/to/repo"}},
				// Local paths match the grammar; MatchesScpLike rejects them.
				{"./rel:path", want{true, "", "./rel", "", "path"}},
				{"/abs/path/with:colon/file", want{true, "", "/abs/path/with", "", "colon/file"}},
				{"@host:p", want{true, "", "@host", "", "p"}},
				{"a@b@host:p", want{true, "a", "b@host", "", "p"}},
				{"host:", want{false, "", "", "", ""}},
				{":path", want{false, "", "", "", ""}},
			},
		},
	} {
		t.Run(rule.name, func(t *testing.T) {
			t.Parallel()
			t.Log(rule.why)

			for _, c := range rule.cases {
				checkEquiv(t, c.in)

				user, host, port, path, ok := matchScpLike(c.in)
				if ok != c.ok {
					t.Errorf("matchScpLike(%q) ok = %v, want %v", c.in, ok, c.ok)
					continue
				}
				if !ok {
					continue
				}
				if user != c.user || host != c.host || port != c.port || path != c.path {
					t.Errorf("matchScpLike(%q) = (user=%q host=%q port=%q path=%q), want (user=%q host=%q port=%q path=%q)",
						c.in, user, host, port, path, c.user, c.host, c.port, c.path)
				}
			}
		})
	}
}

func TestSchemeRules(t *testing.T) {
	t.Parallel()
	t.Log("`^[^:]+://` cannot cross a `:`, so the scheme must end at the FIRST " +
		"one and that colon must open `://`. A later `://` does not count.")

	for _, c := range []struct {
		in   string
		want bool
	}{
		{"a://b", true},
		{"http://host/path", true},
		{"", false},
		{":", false},
		{"://a", false},    // empty scheme
		{"a:/b", false},    // one slash
		{"a:", false},      // nothing after the colon
		{"a:b://c", false}, // the first colon is not the one opening `://`
		{"a\n://b", true},  // `[^:]` matches \n
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
		// Grammar matches; the `/`-before-`:` rule rejects it.
		{"./rel:path", true, false},
		{"/abs/path/with:colon/file", true, false},
		{"sub/dir:foo", true, false},
		// Grammar rejects it outright.
		{"C:\\foo", false, false},
		{"host:", false, false},
	} {
		_, _, _, _, grammar := matchScpLike(c.in)
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
		':', '@', '/', '\\', '1', 'a',
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
// runs without enumerating every intervening byte string.
func TestScannerMatchesRegexpExhaustiveTokens(t *testing.T) {
	t.Parallel()

	tokens := []string{
		"a", "@", ":", "/", "\\", "0", "22", "99999", "123456",
		"\n", "\v", "\t", " ", "\x00", "\xff", "\xc3\xa9",
		"a@", "h:", ":p",
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
	for _, s := range []string{"h:99999:p", "h:123456:p", "a@h:22:p"} {
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
		"host:",      // empty path
		"C:\\foo",    // path opens with a backslash
		"h:p\n",      // trailing newline
		"ho st:path", // whitespace in the host
		":path",      // empty host
		"h:\\",       // path is a lone backslash
		"a@b",        // user branch and no-user branch both lack a colon
	} {
		if oracleScp.FindStringSubmatch(s) != nil {
			t.Fatalf("%q was meant to be a non-match", s)
		}

		user, host, port, path, ok := FindScpLikeComponents(s)
		if ok {
			t.Errorf("FindScpLikeComponents(%q) reported a match", s)
		}
		if user != "" || host != "" || port != "" || path != "" {
			t.Errorf("FindScpLikeComponents(%q) = (%q, %q, %q, %q), want all empty",
				s, user, host, port, path)
		}
	}
}
