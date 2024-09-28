package git

import (
	"fmt"
	"strings"

	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5/storage/memory"
	. "gopkg.in/check.v1"
)

// preReceiveHook returns the bytes of a pre-receive hook script
// that prints m before exiting successfully
func preReceiveHook(m string) []byte {
	return []byte(fmt.Sprintf("#!C:/Program\\ Files/Git/usr/bin/sh.exe\nprintf '%s'\n", m))
}

func (s *RepositorySuite) TestCloneFileUrlWindows(c *C) {
	dir := c.MkDir()

	r, err := PlainInit(dir, false)
	c.Assert(err, IsNil)

	err = util.WriteFile(r.wt, "foo", nil, 0755)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	_, err = w.Commit("foo", &CommitOptions{
		Author:    defaultSignature(),
		Committer: defaultSignature(),
	})
	c.Assert(err, IsNil)

	url := "file:///" + strings.ReplaceAll(dir, "\\", "/")
	c.Assert(url, Matches, "file:///[A-Za-z]:/.*")
	_, err = Clone(memory.NewStorage(), nil, &CloneOptions{
		URL: url,
	})

	c.Assert(err, IsNil)
}
