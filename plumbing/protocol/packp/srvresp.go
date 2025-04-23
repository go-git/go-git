package packp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

const ackLineLen = 44

// ServerResponse object acknowledgement from upload-pack service
type ServerResponse struct {
	ACKs []ACK
}

// ACKStatus represents the status of an object acknowledgement.
type ACKStatus byte

// String returns the string representation of the ACKStatus.
func (s ACKStatus) String() string {
	switch s {
	case ACKContinue:
		return "continue"
	case ACKCommon:
		return "common"
	case ACKReady:
		return "ready"
	}

	return ""
}

// ACKStatus values
const (
	ACKContinue ACKStatus = iota + 1
	ACKCommon
	ACKReady
)

// ACK represents an object acknowledgement. A status can be zero when the
// response doesn't support multi_ack and multi_ack_detailed capabilities.
type ACK struct {
	Hash   plumbing.Hash
	Status ACKStatus
}

// Decode decodes the response into the struct.
func (r *ServerResponse) Decode(reader io.Reader) error {
	var err error
	for err == nil {
		var p []byte
		_, p, err = pktline.ReadLine(reader)
		if err != nil {
			break
		}

		err = r.decodeLine(p)
	}

	if errors.Is(err, io.EOF) {
		return nil
	}

	return err
}

func (r *ServerResponse) decodeLine(line []byte) error {
	if len(line) == 0 {
		return fmt.Errorf("unexpected flush")
	}

	if len(line) >= 3 {
		if bytes.Equal(line[0:3], ack) {
			return r.decodeACKLine(line)
		}

		if bytes.Equal(line[0:3], nak) {
			return io.EOF
		}
	}

	return fmt.Errorf("unexpected content %q", string(line))
}

func (r *ServerResponse) decodeACKLine(line []byte) (err error) {
	parts := bytes.Split(line, []byte(" "))
	if len(line) < ackLineLen || len(parts) < 2 {
		return fmt.Errorf("malformed ACK %q", line)
	}

	var ack ACK
	// TODO: Dynamic hash size and sha256 support
	ack.Hash = plumbing.NewHash(string(bytes.TrimSuffix(parts[1], []byte("\n"))))
	err = io.EOF

	if len(parts) > 2 {
		err = nil
		switch status := strings.TrimSpace(string(parts[2])); status {
		case "continue":
			ack.Status = ACKContinue
		case "common":
			ack.Status = ACKCommon
		case "ready":
			ack.Status = ACKReady
		}
	}

	r.ACKs = append(r.ACKs, ack)
	return
}

// Encode encodes the ServerResponse into a writer.
func (r *ServerResponse) Encode(w io.Writer) error {
	return encodeServerResponse(w, r.ACKs)
}

// encodeServerResponse encodes the ServerResponse into a writer.
func encodeServerResponse(w io.Writer, acks []ACK) error {
	if len(acks) == 0 {
		_, err := pktline.WriteString(w, string(nak)+"\n")
		return err
	}

	var multiAck bool
	for _, a := range acks {
		var err error
		if a.Status > 0 {
			_, err = pktline.Writef(w, "%s %s %s\n", ack, a.Hash, a.Status)
			if !multiAck {
				multiAck = true
			}
		} else {
			_, err = pktline.Writef(w, "%s %s\n", ack, acks[0].Hash)
		}
		if err != nil {
			return err
		}

		if !multiAck {
			break
		}
	}

	return nil
}
