package url

import (
	"runtime"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type URLSuite struct{}

var _ = Suite(&URLSuite{})

func (s *URLSuite) TestMatchesScpLike(c *C) {
	// See https://github.com/git/git/blob/master/Documentation/urls.txt#L37
	examples := []string{
		// Most-extended case
		"git@github.com:james/bond",
		// Most-extended case with port
		"git@github.com:22:james/bond",
		// Most-extended case with numeric path
		"git@github.com:007/bond",
		// Most-extended case with port and numeric "username"
		"git@github.com:22:007/bond",
		// Single repo path
		"git@github.com:bond",
		// Single repo path with port
		"git@github.com:22:bond",
		// Single repo path with port and numeric repo
		"git@github.com:22:007",
		// Repo path ending with .git and starting with _
		"git@github.com:22:_007.git",
		"git@github.com:_007.git",
		"git@github.com:_james.git",
		"git@github.com:_james/bond.git",
	}

	for _, url := range examples {
		c.Check(MatchesScpLike(url), Equals, true)
	}
}

func (s *URLSuite) TestFindScpLikeComponents(c *C) {
	testCases := []struct {
		url, user, host, port, path string
	}{
		{
			// Most-extended case
			url: "git@github.com:james/bond", user: "git", host: "github.com", port: "", path: "james/bond",
		},
		{
			// Most-extended case with port
			url: "git@github.com:22:james/bond", user: "git", host: "github.com", port: "22", path: "james/bond",
		},
		{
			// Most-extended case with numeric path
			url: "git@github.com:007/bond", user: "git", host: "github.com", port: "", path: "007/bond",
		},
		{
			// Most-extended case with port and numeric path
			url: "git@github.com:22:007/bond", user: "git", host: "github.com", port: "22", path: "007/bond",
		},
		{
			// Single repo path
			url: "git@github.com:bond", user: "git", host: "github.com", port: "", path: "bond",
		},
		{
			// Single repo path with port
			url: "git@github.com:22:bond", user: "git", host: "github.com", port: "22", path: "bond",
		},
		{
			// Single repo path with port and numeric path
			url: "git@github.com:22:007", user: "git", host: "github.com", port: "22", path: "007",
		},
		{
			// Repo path ending with .git and starting with _
			url: "git@github.com:22:_007.git", user: "git", host: "github.com", port: "22", path: "_007.git",
		},
		{
			// Repo path ending with .git and starting with _
			url: "git@github.com:_007.git", user: "git", host: "github.com", port: "", path: "_007.git",
		},
		{
			// Repo path ending with .git and starting with _
			url: "git@github.com:_james.git", user: "git", host: "github.com", port: "", path: "_james.git",
		},
		{
			// Repo path ending with .git and starting with _
			url: "git@github.com:_james/bond.git", user: "git", host: "github.com", port: "", path: "_james/bond.git",
		},
	}

	for _, tc := range testCases {
		user, host, port, path := FindScpLikeComponents(tc.url)

		logf := func(ok bool) {
			if ok {
				return
			}
			c.Logf("%q check failed", tc.url)
		}

		logf(c.Check(user, Equals, tc.user))
		logf(c.Check(host, Equals, tc.host))
		logf(c.Check(port, Equals, tc.port))
		logf(c.Check(path, Equals, tc.path))
	}
}

func (s *URLSuite) TestMatchesScpLikeRejectsLocalPaths(c *C) {
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
		c.Check(MatchesScpLike(url), Equals, false, Commentf("%q", url))
	}
}

func (s *URLSuite) TestMatchesScpLikeWindowsDrivePrefix(c *C) {
	// On Windows, drive-letter paths (`C:foo`, `C:/foo`, `C:\foo`)
	// match the SCP regex's host=`C` pattern but are local. Canonical
	// Git rejects them via has_dos_drive_prefix; we mirror that.
	if runtime.GOOS != "windows" {
		c.Skip("Windows-only: drive-prefix disambiguation is platform-specific")
	}
	for _, url := range []string{
		"C:foo",
		"C:/path/to/repo",
		`C:\path\to\repo`,
		"d:relative",
	} {
		c.Check(MatchesScpLike(url), Equals, false, Commentf("%q", url))
	}
}

func (s *URLSuite) TestMatchesScpLikeStillAcceptsRealSCP(c *C) {
	// Regression-guard: the new disambiguation logic must not reject
	// canonical SCP forms.
	for _, url := range []string{
		"git@github.com:james/bond",
		"user@host.example.com:path/to/repo.git",
		"host:path",
	} {
		c.Check(MatchesScpLike(url), Equals, true, Commentf("%q", url))
	}
}
