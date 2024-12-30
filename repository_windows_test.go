package git

import (
	"fmt"
	"strings"

	"github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5/storage/memory"
)

// preReceiveHook returns the bytes of a pre-receive hook script
// that prints m before exiting successfully
func preReceiveHook(m string) []byte {
	return []byte(fmt.Sprintf("#!C:/Program\\ Files/Git/usr/bin/sh.exe\nprintf '%s'\n", m))
}

func (s *RepositorySuite) TestCloneFileUrlWindows() {
	dir := s.T().TempDir()

	r, err := PlainInit(dir, false)
	s.NoError(err)

	err = util.WriteFile(r.wt, "foo", nil, 0755)
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	_, err = w.Add("foo")
	s.NoError(err)

	_, err = w.Commit("foo", &CommitOptions{
		Author:    defaultSignature(),
		Committer: defaultSignature(),
	})
	s.NoError(err)

	url := "file:///" + strings.ReplaceAll(dir, "\\", "/")
	s.Regexp("file:///[A-Za-z]:/.*", url)
	_, err = Clone(memory.NewStorage(), nil, &CloneOptions{
		URL: url,
	})

	s.NoError(err)
}
