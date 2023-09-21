package packp

import (
	"bytes"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"

	. "gopkg.in/check.v1"
)

type UploadPackResponseSuite struct{}

var _ = Suite(&UploadPackResponseSuite{})

func (s *UploadPackResponseSuite) TestDecodeNAK(c *C) {
	raw := "0008NAK\nPACK"

	req := NewUploadPackRequest()
	res := NewUploadPackResponse(req)
	defer res.Close()

	err := res.Decode(io.NopCloser(bytes.NewBufferString(raw)))
	c.Assert(err, IsNil)

	pack, err := io.ReadAll(res)
	c.Assert(err, IsNil)
	c.Assert(pack, DeepEquals, []byte("PACK"))
}

func (s *UploadPackResponseSuite) TestDecodeDepth(c *C) {
	raw := "00000008NAK\nPACK"

	req := NewUploadPackRequest()
	req.Depth = DepthCommits(1)

	res := NewUploadPackResponse(req)
	defer res.Close()

	err := res.Decode(io.NopCloser(bytes.NewBufferString(raw)))
	c.Assert(err, IsNil)

	pack, err := io.ReadAll(res)
	c.Assert(err, IsNil)
	c.Assert(pack, DeepEquals, []byte("PACK"))
}

func (s *UploadPackResponseSuite) TestDecodeMalformed(c *C) {
	raw := "00000008ACK\nPACK"

	req := NewUploadPackRequest()
	req.Depth = DepthCommits(1)

	res := NewUploadPackResponse(req)
	defer res.Close()

	err := res.Decode(io.NopCloser(bytes.NewBufferString(raw)))
	c.Assert(err, NotNil)
}

// multi_ack isn't fully implemented, this ensures that Decode ignores that fact,
// as in some circumstances that's OK to assume so.
//
// TODO: Review as part of multi_ack implementation.
func (s *UploadPackResponseSuite) TestDecodeMultiACK(c *C) {
	req := NewUploadPackRequest()
	req.Capabilities.Set(capability.MultiACK)

	res := NewUploadPackResponse(req)
	defer res.Close()

	err := res.Decode(io.NopCloser(bytes.NewBuffer(nil)))
	c.Assert(err, IsNil)
}

func (s *UploadPackResponseSuite) TestReadNoDecode(c *C) {
	req := NewUploadPackRequest()
	req.Capabilities.Set(capability.MultiACK)

	res := NewUploadPackResponse(req)
	defer res.Close()

	n, err := res.Read(nil)
	c.Assert(err, Equals, ErrUploadPackResponseNotDecoded)
	c.Assert(n, Equals, 0)
}

func (s *UploadPackResponseSuite) TestEncodeNAK(c *C) {
	pf := io.NopCloser(bytes.NewBuffer([]byte("[PACK]")))
	req := NewUploadPackRequest()
	res := NewUploadPackResponseWithPackfile(req, pf)
	defer func() { c.Assert(res.Close(), IsNil) }()

	b := bytes.NewBuffer(nil)
	c.Assert(res.Encode(b), IsNil)

	expected := "0008NAK\n[PACK]"
	c.Assert(b.String(), Equals, expected)
}

func (s *UploadPackResponseSuite) TestEncodeDepth(c *C) {
	pf := io.NopCloser(bytes.NewBuffer([]byte("PACK")))
	req := NewUploadPackRequest()
	req.Depth = DepthCommits(1)

	res := NewUploadPackResponseWithPackfile(req, pf)
	defer func() { c.Assert(res.Close(), IsNil) }()

	b := bytes.NewBuffer(nil)
	c.Assert(res.Encode(b), IsNil)

	expected := "00000008NAK\nPACK"
	c.Assert(b.String(), Equals, expected)
}

func (s *UploadPackResponseSuite) TestEncodeMultiACK(c *C) {
	pf := io.NopCloser(bytes.NewBuffer([]byte("[PACK]")))
	req := NewUploadPackRequest()

	res := NewUploadPackResponseWithPackfile(req, pf)
	defer func() { c.Assert(res.Close(), IsNil) }()
	res.ACKs = []plumbing.Hash{
		plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f81"),
		plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f82"),
	}

	b := bytes.NewBuffer(nil)
	c.Assert(res.Encode(b), NotNil)
}
