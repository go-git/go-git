package git

import (
	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v2/core"
)

type CommitCommon struct{}

var _ = Suite(&CommitCommon{})

func (s *CommitCommon) TestIterClose(c *C) {
	i := &iter{ch: make(chan core.Object, 1)}
	i.Close()
	i.Close()
}
