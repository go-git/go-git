package packp

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// FetchRequestV2 is the client request for the protocol v2 "fetch" command.
type FetchRequestV2 struct {
	// Capabilities are pre-formatted capability lines sent with every v2
	// command request, such as "agent=git/2.40.1" and "object-format=sha1".
	Capabilities []string
	// Wants are the object ids the client wants.
	Wants []plumbing.Hash
	// Haves are object ids the client already has.
	Haves []plumbing.Hash
	// Shallows are the client's current shallow boundary.
	Shallows []plumbing.Hash
	// Depth, when greater than zero, requests a shallow clone to the given
	// depth ("deepen").
	Depth int
	// Filter, when set, requests a partial clone.
	Filter Filter
	// Done tells the server the client is finished negotiating and the
	// server should send the packfile.
	Done bool

	ThinPack   bool
	OfsDelta   bool
	IncludeTag bool
	NoProgress bool
}

// Encode writes the fetch command request to w.
func (r *FetchRequestV2) Encode(w io.Writer) error {
	return writeCommand(w, "fetch", r.Capabilities, func(w io.Writer) error {
		flags := []struct {
			set  bool
			name string
		}{
			{r.ThinPack, "thin-pack"},
			{r.NoProgress, "no-progress"},
			{r.IncludeTag, "include-tag"},
			{r.OfsDelta, "ofs-delta"},
		}
		for _, f := range flags {
			if f.set {
				if _, err := pktline.Writeln(w, f.name); err != nil {
					return err
				}
			}
		}

		for _, shallow := range r.Shallows {
			if _, err := pktline.Writeln(w, "shallow "+shallow.String()); err != nil {
				return err
			}
		}
		if r.Depth > 0 {
			if _, err := pktline.Writeln(w, "deepen "+strconv.Itoa(r.Depth)); err != nil {
				return err
			}
		}
		if r.Filter != "" {
			if _, err := pktline.Writeln(w, "filter "+string(r.Filter)); err != nil {
				return err
			}
		}
		for _, want := range r.Wants {
			if _, err := pktline.Writeln(w, "want "+want.String()); err != nil {
				return err
			}
		}
		for _, have := range r.Haves {
			if _, err := pktline.Writeln(w, "have "+have.String()); err != nil {
				return err
			}
		}
		if r.Done {
			if _, err := pktline.Writeln(w, "done"); err != nil {
				return err
			}
		}
		return nil
	})
}

// FetchResponseV2 holds the parsed sections of a protocol v2 fetch response,
// excluding the packfile itself. When HasPackfile is true, Decode leaves the
// reader positioned at the start of the (sideband-multiplexed) packfile
// stream so it can be streamed into storage.
type FetchResponseV2 struct {
	Acks       []plumbing.Hash
	Ready      bool
	Shallows   []plumbing.Hash
	Unshallows []plumbing.Hash

	HasPackfile bool
}

// Decode reads the non-packfile sections of a fetch response. It stops at
// the terminating flush packet, or at the start of the packfile stream
// (setting HasPackfile), whichever comes first.
func (r *FetchResponseV2) Decode(reader io.Reader) error {
	for {
		l, line, err := pktline.ReadLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch l {
		case pktline.Flush:
			return nil
		case pktline.Delim:
			continue
		}

		var term int
		switch header := strings.TrimRight(string(line), "\n"); header {
		case "acknowledgments":
			term, err = r.decodeAcknowledgments(reader)
		case "shallow-info":
			term, err = r.decodeShallowInfo(reader)
		case "packfile":
			r.HasPackfile = true
			return nil
		case "packfile-uris":
			term, err = r.skipSection(reader)
		default:
			return fmt.Errorf("unexpected fetch response section %q", header)
		}
		if err != nil {
			return err
		}
		if term == pktline.Flush {
			return nil
		}
	}
}

func (r *FetchResponseV2) decodeAcknowledgments(reader io.Reader) (int, error) {
	for {
		l, line, err := pktline.ReadLine(reader)
		if err != nil {
			return 0, err
		}
		if l == pktline.Flush || l == pktline.Delim {
			return l, nil
		}

		s := strings.TrimRight(string(line), "\n")
		switch {
		case s == "NAK":
		case s == "ready":
			r.Ready = true
		case strings.HasPrefix(s, "ACK "):
			r.Acks = append(r.Acks, plumbing.NewHash(strings.TrimPrefix(s, "ACK ")))
		}
	}
}

func (r *FetchResponseV2) decodeShallowInfo(reader io.Reader) (int, error) {
	for {
		l, line, err := pktline.ReadLine(reader)
		if err != nil {
			return 0, err
		}
		if l == pktline.Flush || l == pktline.Delim {
			return l, nil
		}

		s := strings.TrimRight(string(line), "\n")
		switch {
		case strings.HasPrefix(s, "shallow "):
			r.Shallows = append(r.Shallows, plumbing.NewHash(strings.TrimPrefix(s, "shallow ")))
		case strings.HasPrefix(s, "unshallow "):
			r.Unshallows = append(r.Unshallows, plumbing.NewHash(strings.TrimPrefix(s, "unshallow ")))
		}
	}
}

func (r *FetchResponseV2) skipSection(reader io.Reader) (int, error) {
	for {
		l, _, err := pktline.ReadLine(reader)
		if err != nil {
			return 0, err
		}
		if l == pktline.Flush || l == pktline.Delim {
			return l, nil
		}
	}
}
