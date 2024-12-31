package packp

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

// UploadHaves is a message to signal the references that a client has in a
// upload-pack. Done is true when the client has sent a "done" message.
// Otherwise, it means that the client has more haves to send and this request
// was completed with a flush.
type UploadHaves struct {
	Haves []plumbing.Hash
	Done  bool
}

// Encode encodes the UploadHaves into the Writer.
func (u *UploadHaves) Encode(w io.Writer) error {
	plumbing.HashesSort(u.Haves)

	var last plumbing.Hash
	for _, have := range u.Haves {
		if bytes.Equal(last[:], have[:]) {
			continue
		}

		if _, err := pktline.Writef(w, "have %s\n", have); err != nil {
			return fmt.Errorf("sending haves for %q: %w", have, err)
		}

		last = have
	}

	if u.Done {
		if _, err := pktline.Writeln(w, "done"); err != nil {
			return fmt.Errorf("sending done: %s", err)
		}
	} else {
		if err := pktline.WriteFlush(w); err != nil {
			return fmt.Errorf("sending flush-pkt: %s", err)
		}
	}
	return nil
}

// Decode decodes the UploadHaves from the Reader.
func (u *UploadHaves) Decode(r io.Reader) error {
	u.Haves = make([]plumbing.Hash, 0)

	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			if err == io.EOF {
				break
			}

			return fmt.Errorf("decoding haves: %s", err)
		}

		if l == pktline.Flush {
			break
		}

		if bytes.Equal(line, []byte("done\n")) {
			u.Done = true
			break
		}

		if !bytes.HasPrefix(line, []byte("have ")) {
			return fmt.Errorf("invalid have line: %q", line)
		}

		have := plumbing.NewHash(strings.TrimSpace(string(line[5:])))
		u.Haves = append(u.Haves, have)
	}

	return nil
}
