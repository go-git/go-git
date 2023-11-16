package ssh

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/gliderlabs/ssh"
	fixtures "github.com/go-git/go-git-fixtures/v4"
	testutils "github.com/go-git/go-git/v5/plumbing/transport/ssh/internal/test"
	"github.com/go-git/go-git/v5/plumbing/transport/test"
	stdssh "golang.org/x/crypto/ssh"
	. "gopkg.in/check.v1"
)

type ReceivePackSuite struct {
	test.ReceivePackSuite
	fixtures.Suite
	opts []ssh.Option

	port int
	base string
}

var _ = Suite(&ReceivePackSuite{})

func (s *ReceivePackSuite) SetUpSuite(c *C) {
	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, IsNil)

	s.port = l.Addr().(*net.TCPAddr).Port
	s.base, err = os.MkdirTemp(os.TempDir(), fmt.Sprintf("go-git-ssh-%d", s.port))
	c.Assert(err, IsNil)

	DefaultAuthBuilder = func(user string) (AuthMethod, error) {
		return &Password{User: user}, nil
	}

	up := UploadPackSuite{
		base: s.base,
		port: s.port,
	}

	s.ReceivePackSuite.Client = NewClient(&stdssh.ClientConfig{
		HostKeyCallback: stdssh.InsecureIgnoreHostKey(),
	})

	s.ReceivePackSuite.Endpoint = up.prepareRepository(c, fixtures.Basic().One(), "basic.git")
	s.ReceivePackSuite.EmptyEndpoint = up.prepareRepository(c, fixtures.ByTag("empty").One(), "empty.git")
	s.ReceivePackSuite.NonExistentEndpoint = up.newEndpoint(c, "non-existent.git")

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

func (s *ReceivePackSuite) TestSendPackOnEmptyWithReportStatus(c *C) {
	// This test is flaky, so it's disabled until we figure out a solution.
	//
	// packet: < 00af 6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/masterreport-status report-status-v2 delete-refs side-band-64k quiet atomic ofs-delta object-format=sha1 agent=git/2.42.1
	// packet: < 0000
	// packet: > 0075 0000000000000000000000000000000000000000 6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/masterreport-status
	// packet: > 0000
	// packet: < 000a unpack ok
	// packet: < 002a ng refs/heads/master failed to update ref
	// packet: < 0000
	//
	// stderr: error: cannot lock ref 'refs/heads/master': reference already exists
}
