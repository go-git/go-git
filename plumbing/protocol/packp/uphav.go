package packp

import (
	"bytes"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

// UploadHaves is a message to signal the references that a client has in a
// upload-pack. Do not use this directly. Use UploadPackRequest request instead.
type UploadHaves struct {
	Haves []plumbing.Hash
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
			return fmt.Errorf("sending haves for %q: %s", have, err)
		}

		last = have
	}

	return nil
}
