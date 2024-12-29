package packp

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

const ackLineLen = 44

// ServerResponse object acknowledgement from upload-pack service
// TODO: support multi_ack and multi_ack_detailed capabilities
type ServerResponse struct {
	ACKs []plumbing.Hash
}

// Decode decodes the response into the struct, isMultiACK should be true, if
// the request was done with multi_ack or multi_ack_detailed capabilities.
func (r *ServerResponse) Decode(reader io.Reader) error {
	var err error
	for {
		var p []byte
		_, p, err = pktline.ReadLine(reader)
		if err != nil {
			break
		}

		if err := r.decodeLine(p); err != nil {
			return err
		}
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
			return nil
		}
	}

	return fmt.Errorf("unexpected content %q", string(line))
}

func (r *ServerResponse) decodeACKLine(line []byte) error {
	if len(line) < ackLineLen {
		return fmt.Errorf("malformed ACK %q", line)
	}

	sp := bytes.Index(line, []byte(" "))
	h := plumbing.NewHash(string(line[sp+1 : sp+41]))
	r.ACKs = append(r.ACKs, h)
	return nil
}

// Encode encodes the ServerResponse into a writer.
func (r *ServerResponse) Encode(w io.Writer) error {
	if len(r.ACKs) == 0 {
		_, err := pktline.WriteString(w, string(nak)+"\n")
		return err
	}

	_, err := pktline.Writef(w, "%s %s\n", ack, r.ACKs[0].String())
	return err
}
