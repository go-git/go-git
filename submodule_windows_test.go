package git

import (
	"strings"

	. "gopkg.in/check.v1"
)

// TestSubmoduleWindowsAbsoluteURLNotJoined verifies that absolute
// Windows paths in a submodule URL are recognised as absolute and
// therefore skip the relative-URL resolution branch. `path.IsAbs`
// alone returns false for `C:\…` and `\\server\share\…`; the
// production code pairs it with `filepath.IsAbs` so those cases
// don't get wrongly joined onto the superproject's remote URL.
func (s *SubmoduleSuite) TestSubmoduleWindowsAbsoluteURLNotJoined(c *C) {
	for _, url := range []string{
		`C:\path\to\submodule`,
		`\\server\share\submodule`,
	} {
		c.Logf("url: %s", url)

		sm := newSubmoduleForRelativeURL(c,
			"file:///parent/origin", "child", url)

		r, err := sm.Repository()
		c.Assert(err, IsNil)

		remotes, err := r.Remotes()
		c.Assert(err, IsNil)
		c.Assert(remotes, HasLen, 1)

		got := remotes[0].Config().URLs[0]
		c.Assert(strings.Contains(got, "/parent/origin"), Equals, false,
			Commentf("absolute Windows submodule URL was wrongly joined onto the parent's remote: %q", got))
	}
}
