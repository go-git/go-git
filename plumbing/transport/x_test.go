package transport

import (
	"io"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol"
)

func TestConn(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	rwc := &pipeRWC{Reader: pr, Writer: pw}
	s := &testConn{r: pr, w: pw, close: rwc.Close}

	go func() {
		_, err := s.Writer().Write([]byte("hello"))
		assert.NoError(t, err)
	}()

	buf := make([]byte, 5)
	_, err := io.ReadFull(s.Reader(), buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf))

	require.NoError(t, s.Close())
	assert.True(t, rwc.closed)
}

func TestGitProtocolEnv(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", GitProtocolEnv(protocol.V0))
	assert.Equal(t, "version=1", GitProtocolEnv(protocol.V1))
	assert.Equal(t, "version=2", GitProtocolEnv(protocol.V2))
}

func TestRequest(t *testing.T) {
	t.Parallel()

	req := &Request{
		URL:      &url.URL{Scheme: "ssh", Host: "github.com", Path: "/foo/bar.git"},
		Command:  "git-upload-pack",
		Args:     []string{"download"},
		Protocol: protocol.V2,
	}

	assert.Equal(t, "ssh", req.URL.Scheme)
	assert.Equal(t, "/foo/bar.git", req.URL.Path)
	assert.Equal(t, "git-upload-pack", req.Command)
	assert.Equal(t, []string{"download"}, req.Args)
	assert.Equal(t, protocol.V2, req.Protocol)
}

// test helpers

type pipeRWC struct {
	io.Reader
	io.Writer
	closed bool
}

func (p *pipeRWC) Close() error {
	p.closed = true
	if c, ok := p.Reader.(io.Closer); ok {
		c.Close()
	}
	if c, ok := p.Writer.(io.Closer); ok {
		c.Close()
	}
	return nil
}

type testConn struct {
	r     io.Reader
	w     io.WriteCloser
	close func() error
}

func (c *testConn) Reader() io.Reader      { return c.r }
func (c *testConn) Writer() io.WriteCloser { return c.w }
func (c *testConn) Close() error           { return c.close() }
