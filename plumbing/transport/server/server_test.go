package server_test

import (
	"fmt"
	"testing"

	"github.com/src-d/go-git-fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/client"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/server"
	"gopkg.in/src-d/go-git.v4/storage/filesystem"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

const inprocScheme = "inproc"

type BaseSuite struct {
	fixtures.Suite
	loader       server.MapLoader
	client       transport.Transport
	clientBackup transport.Transport
}

func (s *BaseSuite) SetUpSuite(c *C) {
	s.Suite.SetUpSuite(c)
	s.loader = server.MapLoader{}
	s.client = server.NewServer(s.loader)
	s.clientBackup = client.Protocols[inprocScheme]
	client.Protocols[inprocScheme] = s.client
}

func (s *BaseSuite) TearDownSuite(c *C) {
	if s.clientBackup == nil {
		delete(client.Protocols, inprocScheme)
	} else {
		client.Protocols[inprocScheme] = s.clientBackup
	}
}

func (s *BaseSuite) prepareRepositories(c *C, basic *transport.Endpoint,
	empty *transport.Endpoint, nonExistent *transport.Endpoint) {

	f := fixtures.Basic().One()
	fs := f.DotGit()
	path := fs.Base()
	url := fmt.Sprintf("%s://%s", inprocScheme, path)
	ep, err := transport.NewEndpoint(url)
	c.Assert(err, IsNil)
	*basic = ep
	sto, err := filesystem.NewStorage(fs)
	c.Assert(err, IsNil)
	s.loader[ep] = sto

	path = "/empty.git"
	url = fmt.Sprintf("%s://%s", inprocScheme, path)
	ep, err = transport.NewEndpoint(url)
	c.Assert(err, IsNil)
	*empty = ep
	s.loader[ep] = memory.NewStorage()

	path = "/non-existent.git"
	url = fmt.Sprintf("%s://%s", inprocScheme, path)
	ep, err = transport.NewEndpoint(url)
	c.Assert(err, IsNil)
	*nonExistent = ep
}
