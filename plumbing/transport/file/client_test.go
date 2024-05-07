package file

import (
	"context"
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

func (s *ClientSuite) TestCommand(c *C) {
	runner := &runner{}
	ep, err := transport.NewEndpoint(filepath.Join("fake", "repo"))
	var emptyAuth transport.AuthMethod
	c.Assert(err, IsNil)
	_, err = runner.Command(context.TODO(), "git-receive-pack", ep, emptyAuth)
	c.Assert(err, IsNil)

	// Make sure we get an error for one that doesn't exist.
	_, err = runner.Command(context.TODO(), "git-fake-command", ep, emptyAuth)
	c.Assert(err, NotNil)
}

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
