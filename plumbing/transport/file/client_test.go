package file

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ClientSuite struct {
	CommonSuite
}

var _ = Suite(&ClientSuite{})

const bareConfig = `[core]
repositoryformatversion = 0
filemode = true
bare = true`

func prepareRepo(c *C, path string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(path)
	c.Assert(err, IsNil)

	// git-receive-pack refuses to update refs/heads/master on non-bare repo
	// so we ensure bare repo config.
	config := filepath.Join(path, "config")
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
