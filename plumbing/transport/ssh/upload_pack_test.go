package ssh

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	testutils "github.com/go-git/go-git/v5/plumbing/transport/ssh/internal/test"
	"github.com/go-git/go-git/v5/plumbing/transport/test"

	"github.com/gliderlabs/ssh"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	stdssh "golang.org/x/crypto/ssh"
	. "gopkg.in/check.v1"
)

type UploadPackSuite struct {
	test.UploadPackSuite
	fixtures.Suite
	opts []ssh.Option

	port int
	base string
}

var _ = Suite(&UploadPackSuite{})

func (s *UploadPackSuite) SetUpSuite(c *C) {
	if runtime.GOOS == "js" {
		c.Skip("tcp connections are not available in wasm")
	}

	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)

	s.port = l.Addr().(*net.TCPAddr).Port
	s.base = c.MkDir()

	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}

	s.UploadPackSuite.Client = NewClient(&stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	})

	s.UploadPackSuite.Endpoint = s.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.UploadPackSuite.EmptyEndpoint = s.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.UploadPackSuite.NonExistentEndpoint = s.newEndpoint(c, "non-existent.git")

	server := &ssh.Server{
		Handler:     testutils.HandlerSSH(c),
		IdleTimeout: time.Second,
	}
	for _, opt := range s.opts {
		opt(server)
	}
	go func() {
		log.Fatal(server.Serve(l))
	}()
}

func (s *UploadPackSuite) prepareRepository(c *C, f *fixtures.Fixture, name string) *transport.Endpoint {
	fs := f.DotGit()

	err := fixtures.EnsureIsBare(fs)
	c.Assert(err, IsNil)

	path := filepath.Join(s.base, name)
	err = os.Rename(fs.Root(), path)
	c.Assert(err, IsNil)

	return s.newEndpoint(c, name)
}

func (s *UploadPackSuite) newEndpoint(c *C, name string) *transport.Endpoint {
	ep, err := transport.NewEndpoint(fmt.Sprintf(
		"ssh://git@localhost:%d/%s/%s", s.port, filepath.ToSlash(s.base), name,
	))

	c.Assert(err, IsNil)
	return ep
}
