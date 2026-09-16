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
	}

	for _, tc := range testCases {
		user, host, path, ok := FindScpLikeComponents(tc.url)

		s.True(ok, tc.url)
		s.Equal(tc.user, user, tc.url)
		s.Equal(tc.host, host, tc.url)
		s.Equal(tc.path, path, tc.url)
	}
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
