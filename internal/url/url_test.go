package url

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/suite"
)

type URLSuite struct {
	suite.Suite
}

func TestURLSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(URLSuite))
}

func (s *URLSuite) TestMatchesScpLike() {
	// The shape is `[<user>@]<host>:<path>`. See
	// https://github.com/git/git/blob/v2.56.0/Documentation/urls.adoc#L23-L25.
	examples := []string{
		// Most-extended case
		"git@github.com:james/bond",
		// A path whose first segment is digits; there is no port in
		// this form, so `22:` is simply where the path starts.
		"git@github.com:22:james/bond",
		// Most-extended case with numeric path
		"git@github.com:007/bond",
		"git@github.com:22:007/bond",
		// Single repo path
		"git@github.com:bond",
		"git@github.com:22:bond",
		"git@github.com:22:007",
		// Repo path ending with .git and starting with _
		"git@github.com:22:_007.git",
		"git@github.com:_007.git",
		"git@github.com:_james.git",
		"git@github.com:_james/bond.git",
		// Bracketed literal hosts, the only way to write an IPv6
		// address here.
		"[fe80::1]:bond",
		"git@[fe80::1]:james/bond",
		// Neither half has a minimum length. The colon is the whole of
		// the syntax, so all three of these are SSH for canonical Git.
		"host:",
		":path",
		":",
	}

	for _, url := range examples {
		s.True(MatchesScpLike(url))
	}
}

func (s *URLSuite) TestFindScpLikeComponents() {
	// There is no port in the SCP-like form: canonical Git splits the
	// endpoint at the first `:` and everything after it is the path,
	// so a leading digit run in the path stays in the path. See
	// parse_connect_url in
	// https://github.com/git/git/blob/v2.56.0/connect.c#L1097-L1165.
	testCases := []struct {
		url, user, host, path string
	}{
		{
			// Most-extended case
			url: "git@github.com:james/bond", user: "git", host: "github.com", path: "james/bond",
		},
		{
			// A path whose first segment is digits: Git asks the host
			// for `22:james/bond`, it does not dial TCP port 22.
			url: "git@github.com:22:james/bond", user: "git", host: "github.com", path: "22:james/bond",
		},
		{
			// Most-extended case with numeric path
			url: "git@github.com:007/bond", user: "git", host: "github.com", path: "007/bond",
		},
		{
			url: "git@github.com:22:007/bond", user: "git", host: "github.com", path: "22:007/bond",
		},
		{
			// Single repo path
			url: "git@github.com:bond", user: "git", host: "github.com", path: "bond",
		},
		{
			url: "git@github.com:22:bond", user: "git", host: "github.com", path: "22:bond",
		},
		{
			url: "git@github.com:22:007", user: "git", host: "github.com", path: "22:007",
		},
		{
			// Repo path ending with .git and starting with _
			url: "git@github.com:22:_007.git", user: "git", host: "github.com", path: "22:_007.git",
		},
		{
			// Repo path ending with .git and starting with _
			url: "git@github.com:_007.git", user: "git", host: "github.com", path: "_007.git",
		},
		{
			// Repo path ending with .git and starting with _
			url: "git@github.com:_james.git", user: "git", host: "github.com", path: "_james.git",
		},
		{
			// Repo path ending with .git and starting with _
			url: "git@github.com:_james/bond.git", user: "git", host: "github.com", path: "_james/bond.git",
		},
		{
			// A bracketed literal host keeps its brackets, so the host
			// stays usable as a net/url host and as SCP-like text.
			url: "[fe80::1]:bond", user: "", host: "[fe80::1]", path: "bond",
		},
		{
			url: "git@[fe80::1]:james/bond", user: "git", host: "[fe80::1]", path: "james/bond",
		},
		{
			// The colons inside the literal belong to the address, and
			// the one after it still opens the path.
			url: "git@[fe80::1]:22:james/bond", user: "git", host: "[fe80::1]", path: "22:james/bond",
		},
		{
			// git 2.55: HOST=[github.com] CMD=[git-upload-pack ''].
			url: "github.com:", user: "", host: "github.com", path: "",
		},
		{
			// git 2.55: HOST=[] CMD=[git-upload-pack 'james/bond'].
			url: ":james/bond", user: "", host: "", path: "james/bond",
		},
		{
			// git 2.55: HOST=[] CMD=[git-upload-pack ''].
			url: ":", user: "", host: "", path: "",
		},
		{
			// git 2.55 reports the single host `git@`, which ssh then
			// splits the same way this does.
			url: "git@:james/bond", user: "git", host: "", path: "james/bond",
		},
	}

	for _, tc := range testCases {
		user, host, path, ok := FindScpLikeComponents(tc.url)

		s.True(ok, tc.url)
		s.Equal(tc.user, user, tc.url)
		s.Equal(tc.host, host, tc.url)
		s.Equal(tc.path, path, tc.url)
	}
}

func (s *URLSuite) TestMatchesSchemeAtTheFirstSeparator() {
	// Canonical Git splits on the FIRST `://` anywhere in the endpoint
	// (strstr in parse_connect_url) and treats everything before it as
	// the scheme, then refuses the endpoint when it cannot name a
	// protocol. It never falls back to the SCP-like or local reading.
	// See https://github.com/git/git/blob/v2.56.0/connect.c#L1111.
	for _, tc := range []struct {
		url  string
		why  string
		scpe bool
	}{
		// git 2.55: fatal: protocol 'a:b' is not supported
		{url: "a:b://c", why: "the separator does not open at the first colon"},
		// git 2.55: fatal: protocol 'git@host:a' is not supported
		{url: "git@host:a://b", why: "a scheme outranks the SCP-like reading"},
		// git 2.55: fatal: protocol '/abs/a' is not supported
		{url: "/abs/a://b", why: "a scheme outranks the local reading"},
		// git 2.55: fatal: protocol './foo' is not supported
		{url: "./foo://bar", why: "a scheme outranks the local reading"},
		// git 2.55: fatal: protocol '' is not supported
		{url: "://a", why: "an empty scheme is still a scheme"},
	} {
		s.True(MatchesScheme(tc.url), "%s: %s", tc.url, tc.why)
		s.False(IsLocalEndpoint(tc.url), "%s: %s", tc.url, tc.why)
		_, ok := ParseSCP(tc.url)
		s.False(ok, "%s: %s", tc.url, tc.why)
	}

	// A `:` that opens nothing is not a separator, so these keep their
	// SCP-like and local readings.
	for _, url := range []string{"a:/b", "a:", ":", "git@github.com:james/bond"} {
		s.False(MatchesScheme(url), url)
	}
}

func (s *URLSuite) TestParseSCPOmitsAnEmptyUser() {
	// The user is optional in the SCP-like form. url.User("") is an
	// empty userinfo, not the absence of one: String writes it out as
	// a bare `@`, and every `URL.User != nil` test reads it as a user
	// having been given.
	u, ok := ParseSCP("github.com:user/repository.git")
	s.True(ok)
	s.Nil(u.User)
	s.Equal("ssh://github.com/user/repository.git", u.String())

	u, ok = ParseSCP("git@github.com:user/repository.git")
	s.True(ok)
	s.NotNil(u.User)
	s.Equal("git", u.User.Username())
	s.Equal("ssh://git@github.com/user/repository.git", u.String())
}

func (s *URLSuite) TestMatchesScpLikeRejectsLocalPaths() {
	// Cases that look superficially SCP-like but are actually local
	// paths per canonical Git's url_is_local_not_ssh — a `/` before
	// the first `:` means a local path. See
	// https://github.com/git/git/blob/v2.54.0/connect.c#L710-L716.
	for _, url := range []string{
		"/abs/path/with:colon/file",
		"./relative:path",
		"./relative/with:colon",
		"sub/dir:foo",
	} {
		s.False(MatchesScpLike(url), url)
	}
}

func (s *URLSuite) TestMatchesScpLikeWindowsDrivePrefix() {
	// On Windows, drive-letter paths (`C:foo`, `C:/foo`, `C:\foo`)
	// match the SCP regex's host=`C` pattern but are local. Canonical
	// Git rejects them via has_dos_drive_prefix; we mirror that.
	if runtime.GOOS != "windows" {
		s.T().Skip("Windows-only: drive-prefix disambiguation is platform-specific")
	}
	for _, url := range []string{
		"C:foo",
		"C:/path/to/repo",
		`C:\path\to\repo`,
		"d:relative",
	} {
		s.False(MatchesScpLike(url), url)
	}
}

func (s *URLSuite) TestMatchesScpLikeDrivePrefixOffWindows() {
	// Off Windows a drive-letter path is NOT local: Git's
	// has_dos_drive_prefix is a no-op there (git-compat-util.h), so
	// url_is_local_not_ssh never reaches the local reading and Git
	// dials host `C`. git 2.55 on macOS, for `C:\path\to\repo`:
	// HOST=[C] CMD=[git-upload-pack '\path\to\repo'].
	if runtime.GOOS == "windows" {
		s.T().Skip("non-Windows only: the drive prefix IS local on Windows")
	}
	for _, url := range []string{
		"C:foo",
		"C:/path/to/repo",
		`C:\path\to\repo`,
		"d:relative",
	} {
		s.True(MatchesScpLike(url), url)
	}
}

func (s *URLSuite) TestMatchesScpLikeAcceptsAnyByte() {
	// Git restricts neither half of the SCP-like form, so neither does
	// this. go-git used to reject whitespace in the host and a leading
	// backslash or an embedded newline in the path, which did not
	// reject the endpoint -- it reclassified it as a local path.
	for _, url := range []string{
		"ho st:path",    // git 2.55: HOST=[ho st] CMD=[...'path']
		"host:\\path",   // git 2.55: HOST=[host] CMD=[...'\path']
		"host:a\nb",     // git 2.55: HOST=[host] CMD=[...'a\nb']
		"host:path\n",   // git 2.55: HOST=[host] CMD=[...'path\n']
		"[a b]:c",       // git 2.55: HOST=[a b] CMD=[...'c']
		"ho\x00st:path", // NUL and invalid UTF-8 are ordinary bytes
		"ho\xffst:path",
	} {
		s.True(MatchesScpLike(url), "%q", url)
	}
}

func (s *URLSuite) TestMatchesScpLikeStillAcceptsRealSCP() {
	// Regression-guard: the new disambiguation logic must not reject
	// canonical SCP forms.
	for _, url := range []string{
		"git@github.com:james/bond",
		"user@host.example.com:path/to/repo.git",
		"host:path",
		"[fe80::1]:path",
		"git@[fe80::1]:path",
	} {
		s.True(MatchesScpLike(url), url)
	}
}
