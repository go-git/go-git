package packp

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// Encode writes the UlReq encoding of u to the stream.
//
// All the payloads will end with a newline character.  Wants and
// shallows are sorted alphabetically.  A depth of 0 means no depth
// request is sent.
func (req *UploadRequest) Encode(w io.Writer) error {
	if len(req.Wants) == 0 {
		return fmt.Errorf("empty wants provided")
	}

	plumbing.HashesSort(req.Wants)

	// First want line (with optional capabilities)
	if req.Capabilities.IsEmpty() {
		if _, err := pktline.Writef(w, "want %s\n", req.Wants[0]); err != nil {
			return fmt.Errorf("encoding first want line: %s", err)
		}
	} else {
		if _, err := pktline.Writef(w, "want %s %s\n",
			req.Wants[0],
			req.Capabilities.String(),
		); err != nil {
			return fmt.Errorf("encoding first want line: %s", err)
		}
	}

	// Additional wants (deduplicated)
	last := req.Wants[0]
	for _, h := range req.Wants[1:] {
		if last.Compare(h.Bytes()) == 0 {
			continue
		}
		if _, err := pktline.Writef(w, "want %s\n", h); err != nil {
			return fmt.Errorf("encoding want %q: %s", h, err)
		}
		last = h
	}

	// Shallows (sorted, deduplicated)
	plumbing.HashesSort(req.Shallows)
	var lastShallow plumbing.Hash
	for _, s := range req.Shallows {
		if lastShallow.Compare(s.Bytes()) == 0 {
			continue
		}
		if _, err := pktline.Writef(w, "shallow %s\n", s); err != nil {
			return fmt.Errorf("encoding shallow %q: %s", s, err)
		}
		lastShallow = s
	}

	// Depth
	depth := req.Depth
	if depth.Deepen > 0 && (!depth.DeepenSince.IsZero() || len(depth.DeepenNot) > 0) {
		return ErrDeepenMutuallyExclusive
	}
	if depth.Deepen > 0 {
		if _, err := pktline.Writef(w, "deepen %d\n", depth.Deepen); err != nil {
			return fmt.Errorf("encoding depth %d: %s", depth.Deepen, err)
		}
	}
	if !depth.DeepenSince.IsZero() {
		when := depth.DeepenSince.UTC()
		if _, err := pktline.Writef(w, "deepen-since %d\n", when.Unix()); err != nil {
			return fmt.Errorf("encoding depth %s: %s", when, err)
		}
	}
	for _, ref := range depth.DeepenNot {
		if _, err := pktline.Writef(w, "deepen-not %s\n", ref); err != nil {
			return fmt.Errorf("encoding depth %s: %s", ref, err)
		}
	}

	// Filter
	if filter := req.Filter; filter != "" {
		if _, err := pktline.Writef(w, "filter %s\n", filter); err != nil {
			return fmt.Errorf("encoding filter %s: %s", filter, err)
		}
	}

	return pktline.WriteFlush(w)
}
