package file

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"gopkg.in/src-d/go-git.v4/plumbing/transport"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

const bareConfig = `[core]
repositoryformatversion = 0
filemode = true
bare = true`

func prepareRepo(c *C, path string) transport.Endpoint {
	url := fmt.Sprintf("file://%s", path)
	ep, err := transport.NewEndpoint(url)
	c.Assert(err, IsNil)

	// git-receive-pack refuses to update refs/heads/master on non-bare repo
	// so we ensure bare repo config.
	config := fmt.Sprintf("%s/config", path)
	if _, err := os.Stat(config); err == nil {
		f, err := os.OpenFile(config, os.O_TRUNC|os.O_WRONLY, 0)
		c.Assert(err, IsNil)
		content := strings.NewReader(bareConfig)
		_, err = io.Copy(f, content)
		c.Assert(err, IsNil)
		c.Assert(f.Close(), IsNil)
	}

	return ep
}
