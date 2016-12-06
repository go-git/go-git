package packp

import (
	"bytes"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/plumbing/protocol/packp/capability"

	. "gopkg.in/check.v1"
)

type UploadPackResponseSuite struct{}

var _ = Suite(&UploadPackResponseSuite{})

func (s *UploadPackResponseSuite) TestDecodeNAK(c *C) {
	raw := "0008NAK\n[PACK]"

	req := NewUploadPackRequest()
	res := NewUploadPackResponse(req)
	defer res.Close()

	err := res.Decode(ioutil.NopCloser(bytes.NewBufferString(raw)))
	c.Assert(err, IsNil)

	pack, err := ioutil.ReadAll(res)
	c.Assert(err, IsNil)
	c.Assert(pack, DeepEquals, []byte("[PACK]"))
}

func (s *UploadPackResponseSuite) TestDecodeDepth(c *C) {
	raw := "00000008NAK\n[PACK]"

	req := NewUploadPackRequest()
	req.Depth = DepthCommits(1)

	res := NewUploadPackResponse(req)
	defer res.Close()

	err := res.Decode(ioutil.NopCloser(bytes.NewBufferString(raw)))
	c.Assert(err, IsNil)

	pack, err := ioutil.ReadAll(res)
	c.Assert(err, IsNil)
	c.Assert(pack, DeepEquals, []byte("[PACK]"))
}

func (s *UploadPackResponseSuite) TestDecodeMalformed(c *C) {
	raw := "00000008ACK\n[PACK]"

	req := NewUploadPackRequest()
	req.Depth = DepthCommits(1)

	res := NewUploadPackResponse(req)
	defer res.Close()

	err := res.Decode(ioutil.NopCloser(bytes.NewBufferString(raw)))
	c.Assert(err, NotNil)
}

func (s *UploadPackResponseSuite) TestDecodeMultiACK(c *C) {
	req := NewUploadPackRequest()
	req.Capabilities.Set(capability.MultiACK)

	res := NewUploadPackResponse(req)
	defer res.Close()

	err := res.Decode(ioutil.NopCloser(bytes.NewBuffer(nil)))
	c.Assert(err, NotNil)
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
