package file

import (
	"context"
	"testing"

	"github.com/go-git/go-git/v5/internal/transport/test"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

type UploadPackSuite struct {
	CommonSuite
	test.UploadPackSuite
}

var _ = Suite(&UploadPackSuite{})

func (s *UploadPackSuite) SetUpSuite(c *C) {
	// trace.SetLogger(log.Default())
	// trace.SetTarget(trace.General | trace.Packet)
	s.CommonSuite.SetUpSuite(c)

	s.UploadPackSuite.Client = DefaultTransport

	fixture := fixtures.Basic().One()
	dot := fixture.DotGit()
	path := dot.Root()
	ep, err := transport.NewEndpoint(path)
	c.Assert(err, IsNil)
	s.Endpoint = ep
	s.Storer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	fixture = fixtures.ByTag("empty").One()
	dot = fixture.DotGit()
	path = dot.Root()
	ep, err = transport.NewEndpoint(path)
	c.Assert(err, IsNil)
	s.EmptyEndpoint = ep
	s.EmptyStorer = filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	ep, err = transport.NewEndpoint("non-existent")
	c.Assert(err, IsNil)
	s.NonExistentEndpoint = ep
	s.NonExistentStorer = memory.NewStorage()
}

func TestAFile(t *testing.T) {
	// fixture := fixtures.Basic().One()
	fixture := fixtures.ByTag("empty").One()
	dot := fixture.DotGit()
	path := dot.Root()
	ep, err := transport.NewEndpoint(path)
	if err != nil {
		t.Fatal(err)
	}

	// st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	st := memory.NewStorage()

	r, err := DefaultTransport.NewSession(st, ep, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.Handshake(context.TODO(), transport.UploadPackService)
	if err == nil {
		t.Fatal("expected error")
	}
}
