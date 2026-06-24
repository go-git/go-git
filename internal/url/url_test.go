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
		s.True(MatchesScpLike(url))
	}
}

func (s *URLSuite) TestFindScpLikeComponents() {
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

		s.Equal(tc.user, user, tc.url)
		s.Equal(tc.host, host, tc.url)
		s.Equal(tc.port, port, tc.url)
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
	} {
		s.True(MatchesScpLike(url), url)
	}
}
