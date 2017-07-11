package packp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/pktline"
)

const ackLineLen = 44

// ServerResponse object acknowledgement from upload-pack service
type ServerResponse struct {
	ACKs []plumbing.Hash
}

// Decode decodes the response into the struct, isMultiACK should be true, if
// the request was done with multi_ack or multi_ack_detailed capabilities.
func (r *ServerResponse) Decode(reader *bufio.Reader, isMultiACK bool) error {
	// TODO: implement support for multi_ack or multi_ack_detailed responses
	if isMultiACK {
		return errors.New("multi_ack and multi_ack_detailed are not supported")
	}

	s := pktline.NewScanner(reader)

	for s.Scan() {
		line := s.Bytes()

		if err := r.decodeLine(line); err != nil {
			return err
		}

		// we need to detect when the end of a response header and the begining
		// of a packfile header happend, some requests to the git daemon
		// produces a duplicate ACK header even when multi_ack is not supported.
		isEnd, err := r.endReached(reader)
		if err != nil {
			return err
		}

		if isEnd {
			break
		}
	}

	return s.Err()
}

func (r *ServerResponse) endReached(reader *bufio.Reader) (bool, error) {
	isPack, err := r.isPACKHeader(reader)
	if err == io.EOF {
		return true, nil
	}

	return isPack, err

}

// isPACKHeader detects when a header of a packfile is found, with this goal
// the function is reading from the reader without moving the read pointer.
func (r *ServerResponse) isPACKHeader(reader *bufio.Reader) (bool, error) {
	ahead, err := reader.Peek(9)
	if err != nil {
		return false, err
	}

	if len(ahead) == 0 {
		return true, nil
	}

	if len(ahead) > 4 && string(ahead[0:4]) == "PACK" {
		return true, nil
	}

	if len(ahead) == 9 && string(ahead[5:]) == "PACK" {
		return true, nil
	}

	return true, nil

}

func (r *ServerResponse) decodeLine(line []byte) error {
	if len(line) == 0 {
		return fmt.Errorf("unexpected flush")
	}

	if bytes.Compare(line[0:3], ack) == 0 {
		return r.decodeACKLine(line)
	}

	if bytes.Compare(line[0:3], nak) == 0 {
		return nil
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
	if len(r.ACKs) > 1 {
		return errors.New("multi_ack and multi_ack_detailed are not supported")
	}

	e := pktline.NewEncoder(w)
	if len(r.ACKs) == 0 {
		return e.Encodef("%s\n", nak)
	}

	return e.Encodef("%s %s\n", ack, r.ACKs[0].String())
}
