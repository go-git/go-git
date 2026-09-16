package url

import (
	"regexp"
	"testing"
)

// FuzzURLScanner compares the URL scanners with equivalent regular
// expressions, checking both matches and extracted components.
//
// The oracle expressions and seed corpus are local to the target because
// OSS-Fuzz builds it without the package's other test files. See tests/fuzz.
func FuzzURLScanner(f *testing.F) {
	oracleScheme := regexp.MustCompile(`^[^:]+://`)
	oracleScp := regexp.MustCompile(`^(?:(?P<user>[^@]+)@)?(?P<host>[^:\s]+):(?:(?P<port>[0-9]{1,5}):)?(?P<path>[^\\].*)$`)

	for _, seed := range []string{
		"", ":", "://", "a://", "a://b", "a:b://c", "://a", "a:/b", "a:",
		"ssh://git@github.com/user/repository.git",
		"http://git:pass@github.com:8080/user/repository.git?foo#bar",
		"a@b:c", "a@b@c:d", "@host:p", "a@:path", "a@@b:c", "@:p", "a@b",
		"host:path", ":path", "host:", "ho st:path", "ho\tst:path",
		"ho\nst:path", "ho\rst:path", "ho\fst:path", "ho\vst:path",
		"ho\x00st:path", "ho\xffst:path", "h\xc3\xa9st:path",
		"h:22:p", "h:0:p", "h:99999:p", "h:123456:p", "h:22:", "h:22:\\p",
		"h:007/bond", "h::p", "h:2a:p",
		"h:\\p", "h:p\nq", "h:p\n", "h:\np", "h:\n", "h:\\", "h:p\\q",
		"h:\xc3\xa9p", "h:\xc3\xa9\n",
		"git@github.com:james/bond", "git@github.com:22:james/bond",
		"git@github.com:22:007/bond", "git@github.com:_james/bond.git",
		"user@host.example.com:path/to/repo.git",
		"/abs/path/with:colon/file", "./relative:path", "sub/dir:foo",
		"C:foo", "C:/path/to/repo", "C:\\path\\to\\repo", "d:relative",
		"/foo.git", "foo.git", "file:///foo.git", "file://C:/path/to/repo",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, endpoint string) {
		if got, want := MatchesScheme(endpoint), oracleScheme.MatchString(endpoint); got != want {
			t.Fatalf("MatchesScheme(%q) = %v, regexp says %v", endpoint, got, want)
		}

		user, host, port, path, ok := matchScpLike(endpoint)
		m := oracleScp.FindStringSubmatch(endpoint)
		if ok != (m != nil) {
			t.Fatalf("matchScpLike(%q) ok = %v, regexp says %v", endpoint, ok, m != nil)
		}
		if !ok {
			return
		}
		if user != m[1] || host != m[2] || port != m[3] || path != m[4] {
			t.Fatalf("matchScpLike(%q) = (user=%q host=%q port=%q path=%q), regexp = (user=%q host=%q port=%q path=%q)",
				endpoint, user, host, port, path, m[1], m[2], m[3], m[4])
		}
	})
}
