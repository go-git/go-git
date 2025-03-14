package packp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// ErrInvalidSmartReply is returned when a SmartReply is invalid.
var ErrInvalidSmartReply = errors.New("invalid smart reply")

// SmartReply represents Git HTTP smart protocol service payload.
//
// When sending a message over (smart) HTTP, you have to add a pktline before
// the whole thing with the following payload:
//
// '# service=$servicename" LF
//
// Moreover, some if not all, git HTTP smart servers will send a flush-pkt just
// after the first pkt-line.
type SmartReply struct {
	Service string
}

// Decode decodes a SmartReply from reader.
func (s *SmartReply) Decode(r io.Reader) error {
	_, p, err := pktline.ReadLine(r)
	if err != nil {
		return err
	}

	if len(p) == 0 || !bytes.HasPrefix(p, []byte("# service=")) {
		return fmt.Errorf("%w: %q", ErrInvalidSmartReply, p)
	}

	s.Service = strings.TrimSpace(string(p[10:]))
	l, _, err := pktline.ReadLine(r)
	if err != nil {
		return err
	}

	if l != pktline.Flush {
		return fmt.Errorf("%w: expected flush-pkt", ErrInvalidSmartReply)
	}

	return nil
}

// Encode encodes a SmartReply to writer.
func (s *SmartReply) Encode(w io.Writer) error {
	if _, err := pktline.Writef(w, "# service=%s\n", s.Service); err != nil {
		return err
	}

	return pktline.WriteFlush(w)
}
