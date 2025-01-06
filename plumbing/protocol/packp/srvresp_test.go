package packp

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/stretchr/testify/suite"
)

type ServerResponseSuite struct {
	suite.Suite
}

func TestServerResponseSuite(t *testing.T) {
	suite.Run(t, new(ServerResponseSuite))
}

func (s *ServerResponseSuite) TestDecodeNAK() {
	raw := "0008NAK\n"

	sr := &ServerResponse{}
	err := sr.Decode((bytes.NewBufferString(raw)), false)
	s.NoError(err)

	s.Len(sr.ACKs, 0)
}

func (s *ServerResponseSuite) TestDecodeNewLine() {
	raw := "\n"

	sr := &ServerResponse{}
	err := sr.Decode(bytes.NewBufferString(raw), false)
	s.NotNil(err)
	s.Regexp(regexp.MustCompile("invalid pkt-len found.*"), err.Error())
}

func (s *ServerResponseSuite) TestDecodeEmpty() {
	raw := ""

	sr := &ServerResponse{}
	err := sr.Decode(bytes.NewBufferString(raw), false)
	s.NoError(err)
}

func (s *ServerResponseSuite) TestDecodePartial() {
	raw := "000600\n"

	sr := &ServerResponse{}
	err := sr.Decode(bytes.NewBufferString(raw), false)
	s.NotNil(err)
	s.Equal(fmt.Sprintf("unexpected content %q", "00"), err.Error())
}

func (s *ServerResponseSuite) TestDecodeACK() {
	raw := "0031ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n"

	sr := &ServerResponse{}
	err := sr.Decode(bytes.NewBufferString(raw), false)
	s.NoError(err)

	s.Len(sr.ACKs, 1)
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), sr.ACKs[0])
}

func (s *ServerResponseSuite) TestDecodeMultipleACK() {
	raw := "" +
		"0031ACK 1111111111111111111111111111111111111111\n" +
		"0031ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n" +
		"00080PACK\n"

	sr := &ServerResponse{}
	err := sr.Decode(bytes.NewBufferString(raw), false)
	s.NoError(err)

	s.Len(sr.ACKs, 2)
	s.Equal(plumbing.NewHash("1111111111111111111111111111111111111111"), sr.ACKs[0])
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), sr.ACKs[1])
}

func (s *ServerResponseSuite) TestDecodeMultipleACKWithSideband() {
	raw := "" +
		"0031ACK 1111111111111111111111111111111111111111\n" +
		"0031ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n" +
		"00080aaaa\n"

	sr := &ServerResponse{}
	err := sr.Decode(bytes.NewBufferString(raw), false)
	s.NoError(err)

	s.Len(sr.ACKs, 2)
	s.Equal(plumbing.NewHash("1111111111111111111111111111111111111111"), sr.ACKs[0])
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), sr.ACKs[1])
}

func (s *ServerResponseSuite) TestDecodeMalformed() {
	raw := "0029ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e\n"

	sr := &ServerResponse{}
	err := sr.Decode(bytes.NewBufferString(raw), false)
	s.NotNil(err)
}

func (s *ServerResponseSuite) TestDecodeMultiACK() {
	raw := "" +
		"0031ACK 1111111111111111111111111111111111111111\n" +
		"0031ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n" +
		"00080PACK\n"

	sr := &ServerResponse{}
	err := sr.Decode(strings.NewReader(raw), true)
	s.NoError(err)

	s.Len(sr.ACKs, 2)
	s.Equal(plumbing.NewHash("1111111111111111111111111111111111111111"), sr.ACKs[0])
	s.Equal(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), sr.ACKs[1])
}

func (s *ServerResponseSuite) TestEncodeEmpty() {
	haves := make(chan UploadPackCommand)
	go func() {
		haves <- UploadPackCommand{
			Acks: []UploadPackRequestAck{},
			Done: true,
		}
		close(haves)
	}()
	sr := &ServerResponse{req: &UploadPackRequest{UploadPackCommands: haves, UploadRequest: UploadRequest{Capabilities: capability.NewList()}}}
	b := bytes.NewBuffer(nil)
	err := sr.Encode(b)
	s.NoError(err)

	s.Equal("0008NAK\n", b.String())
}

func (s *ServerResponseSuite) TestEncodeSingleAck() {
	haves := make(chan UploadPackCommand)
	go func() {
		haves <- UploadPackCommand{
			Acks: []UploadPackRequestAck{
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e1")},
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e2")},
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e3"), IsCommon: true},
			}}
		close(haves)
	}()
	sr := &ServerResponse{req: &UploadPackRequest{UploadPackCommands: haves, UploadRequest: UploadRequest{Capabilities: capability.NewList()}}}
	b := bytes.NewBuffer(nil)
	err := sr.Encode(b)
	s.NoError(err)

	s.Equal("0031ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e3\n", b.String())
}

func (s *ServerResponseSuite) TestEncodeSingleAckDone() {
	haves := make(chan UploadPackCommand)
	go func() {
		haves <- UploadPackCommand{
			Acks: []UploadPackRequestAck{
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e1")},
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e2")},
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e3"), IsCommon: true},
			},
			Done: true,
		}
		close(haves)
	}()
	sr := &ServerResponse{req: &UploadPackRequest{UploadPackCommands: haves, UploadRequest: UploadRequest{Capabilities: capability.NewList()}}}
	b := bytes.NewBuffer(nil)
	err := sr.Encode(b)
	s.NoError(err)

	s.Equal("0031ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e3\n", b.String())
}

func (s *ServerResponseSuite) TestEncodeMutiAck() {
	haves := make(chan UploadPackCommand)
	go func() {
		haves <- UploadPackCommand{
			Acks: []UploadPackRequestAck{
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e1")},
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e2"), IsCommon: true, IsReady: true},
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e3")},
			},
		}
		haves <- UploadPackCommand{
			Acks: []UploadPackRequestAck{},
			Done: true,
		}
		close(haves)
	}()
	capabilities := capability.NewList()
	capabilities.Add(capability.MultiACK)
	sr := &ServerResponse{req: &UploadPackRequest{UploadPackCommands: haves, UploadRequest: UploadRequest{Capabilities: capabilities}}}
	b := bytes.NewBuffer(nil)
	err := sr.Encode(b)
	s.NoError(err)

	lines := strings.Split(b.String(), "\n")
	s.Len(lines, 5)
	s.Equal("003aACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e2 continue", lines[0])
	s.Equal("003aACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e3 continue", lines[1])
	s.Equal("0008NAK", lines[2])
	s.Equal("0031ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e3", lines[3])
	s.Equal("", lines[4])
}

func (s *ServerResponseSuite) TestEncodeMutiAckOnlyOneNak() {
	haves := make(chan UploadPackCommand)
	go func() {
		haves <- UploadPackCommand{
			Acks: []UploadPackRequestAck{}, //no common hash
			Done: true,
		}
		close(haves)
	}()
	capabilities := capability.NewList()
	capabilities.Add(capability.MultiACK)
	sr := &ServerResponse{req: &UploadPackRequest{UploadPackCommands: haves, UploadRequest: UploadRequest{Capabilities: capabilities}}}
	b := bytes.NewBuffer(nil)
	err := sr.Encode(b)
	s.NoError(err)

	lines := strings.Split(b.String(), "\n")
	s.Len(lines, 2)
	s.Equal("0008NAK", lines[0])
	s.Equal("", lines[1])
}

func (s *ServerResponseSuite) TestEncodeMutiAckDetailed() {
	haves := make(chan UploadPackCommand)
	go func() {
		haves <- UploadPackCommand{
			Acks: []UploadPackRequestAck{
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e1")},
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e2"), IsCommon: true, IsReady: true},
				{Hash: plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e3"), IsCommon: true},
			},
		}
		haves <- UploadPackCommand{
			Acks: []UploadPackRequestAck{},
			Done: true,
		}
		close(haves)
	}()
	capabilities := capability.NewList()
	capabilities.Add(capability.MultiACKDetailed)
	sr := &ServerResponse{req: &UploadPackRequest{UploadPackCommands: haves, UploadRequest: UploadRequest{Capabilities: capabilities}}}
	b := bytes.NewBuffer(nil)
	err := sr.Encode(b)
	s.NoError(err)

	lines := strings.Split(b.String(), "\n")
	s.Len(lines, 5)
	s.Equal("0037ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e2 ready", lines[0])
	s.Equal("0038ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e3 common", lines[1])
	s.Equal("0008NAK", lines[2])
	s.Equal("0031ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e3", lines[3])
	s.Equal("", lines[4])
}
