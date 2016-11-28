package packp

import (
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/pktline"
)

// UploadHaves is a message to signal the references that a client has in a
// upload-pack. Do not use this directly. Use UploadPackRequest request instead.
type UploadHaves struct {
	Haves []plumbing.Hash
}

// Encode encodes the UploadHaves into the Writer.
func (u *UploadHaves) Encode(w io.Writer) error {
	e := pktline.NewEncoder(w)
	for _, have := range u.Haves {
		if err := e.Encodef("have %s\n", have); err != nil {
			return fmt.Errorf("sending haves for %q: %s", have, err)
		}
	}

	if len(u.Haves) != 0 {
		if err := e.Flush(); err != nil {
			return fmt.Errorf("sending flush-pkt after haves: %s", err)
		}
	}

	return nil
}

// Have adds a hash reference to the 'haves' list.
func (r *UploadHaves) Have(h ...plumbing.Hash) {
	r.Haves = append(r.Haves, h...)
}

// UploadPackRequest represents a upload-pack request.
// Zero-value is not safe, use NewUploadPackRequest instead.
type UploadPackRequest struct {
	*UploadRequest
	*UploadHaves
}

// NewUploadPackRequest creates a new UploadPackRequest and returns a pointer.
func NewUploadPackRequest() *UploadPackRequest {
	return &UploadPackRequest{
		UploadHaves:   &UploadHaves{},
		UploadRequest: NewUploadRequest(),
	}
}

func (r *UploadPackRequest) IsEmpty() bool {
	return isSubset(r.Wants, r.Haves)
}

func isSubset(needle []plumbing.Hash, haystack []plumbing.Hash) bool {
	for _, h := range needle {
		found := false
		for _, oh := range haystack {
			if h == oh {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}
