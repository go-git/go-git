package http

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/test"

	"github.com/src-d/go-git-fixtures"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	test.ReceivePackSuite
	fixtures.Suite

	base string
	host string
	port int
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpTest(c *C) {
	s.ReceivePackSuite.Client = DefaultClient

	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)

	base, err := ioutil.TempDir(os.TempDir(), "go-git-http-backend-test")
	c.Assert(err, IsNil)

	s.port = l.Addr().(*net.TCPAddr).Port
	s.host = fmt.Sprintf("localhost_%d", s.port)
	s.base = filepath.Join(base, s.host)

	err = os.MkdirAll(s.base, 0755)
	c.Assert(err, IsNil)

	s.ReceivePackSuite.Endpoint = s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.ReceivePackSuite.EmptyEndpoint = s.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.ReceivePackSuite.NonExistentEndpoint = s.newEndpoint(c, "non-existent.git")

	cmd := exec.Command("git", "--exec-path")
	out, err := cmd.CombinedOutput()
	c.Assert(err, IsNil)

	server := &http.Server{
		Handler: &cgi.Handler{
			Path: filepath.Join(strings.Trim(string(out), "\n"), "git-http-backend"),
			Env:  []string{"GIT_HTTP_EXPORT_ALL=true", fmt.Sprintf("GIT_PROJECT_ROOT=%s", s.base)},
		},
	}
	go func() {
		log.Fatal(server.Serve(l))
	}()
}

func (s *ReceivePackSuite) TearDownTest(c *C) {
	err := os.RemoveAll(s.base)
	c.Assert(err, IsNil)
}

func (s *ReceivePackSuite) prepareRepository(c *C, f *fixtures.Fixture, name string) transport.Endpoint {
	path := filepath.Join(s.base, name)

	err := os.Rename(f.DotGit().Root(), path)
	c.Assert(err, IsNil)

	s.setConfigToRepository(c, path)
	return s.newEndpoint(c, name)
}

// git-receive-pack refuses to update refs/heads/master on non-bare repo
// so we ensure bare repo config.
func (s *ReceivePackSuite) setConfigToRepository(c *C, path string) {
	cfgPath := filepath.Join(path, "config")
	_, err := os.Stat(cfgPath)
	c.Assert(err, IsNil)

	cfg, err := os.OpenFile(cfgPath, os.O_TRUNC|os.O_WRONLY, 0)
	c.Assert(err, IsNil)

	content := strings.NewReader("" +
		"[core]\n" +
		"repositoryformatversion = 0\n" +
		"filemode = true\n" +
		"bare = true\n" +
		"[http]\n" +
		"receivepack = true\n",
	)

	_, err = io.Copy(cfg, content)
	c.Assert(err, IsNil)

	c.Assert(cfg.Close(), IsNil)
}

func (s *ReceivePackSuite) newEndpoint(c *C, name string) transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf("http://localhost:%d/%s", s.port, name))
	c.Assert(err, IsNil)

	return ep
}
