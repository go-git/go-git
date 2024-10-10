package packp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

const ackLineLen = 44

// ServerResponse object acknowledgement from upload-pack service
type ServerResponse struct {
	ACKs []plumbing.Hash
}

// Decode decodes the response into the struct, isMultiACK should be true, if
// the request was done with multi_ack or multi_ack_detailed capabilities.
func (r *ServerResponse) Decode(reader io.Reader, isMultiACK bool) error {
	s := bufio.NewReader(reader)

	var err error
	for {
		var p []byte
		_, p, err = pktline.ReadLine(s)
		if err != nil {
			break
		}

		if err := r.decodeLine(p); err != nil {
			return err
		}

		// we need to detect when the end of a response header and the beginning
		// of a packfile header happened, some requests to the git daemon
		// produces a duplicate ACK header even when multi_ack is not supported.
		stop, err := r.stopReading(s)
		if err != nil {
			return err
		}

		if stop {
			break
		}
	}

	if err == io.EOF {
		return nil
	}

	return err
}

// stopReading detects when a valid command such as ACK or NAK is found to be
// read in the buffer without moving the read pointer.
func (r *ServerResponse) stopReading(reader ioutil.ReadPeeker) (bool, error) {
	ahead, err := reader.Peek(7)
	if err == io.EOF {
		return true, nil
	}

	if err != nil {
		return false, err
	}

	if len(ahead) > 4 && r.isValidCommand(ahead[0:3]) {
		return false, nil
	}

	if len(ahead) == 7 && r.isValidCommand(ahead[4:]) {
		return false, nil
	}

	return true, nil
}

func (r *ServerResponse) isValidCommand(b []byte) bool {
	commands := [][]byte{ack, nak}
	for _, c := range commands {
		if bytes.Equal(b, c) {
			return true
		}
	}

	return false
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
