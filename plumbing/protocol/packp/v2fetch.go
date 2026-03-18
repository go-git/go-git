package packp

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

// V2FetchRequest represents a V2 fetch command request.
//
// Wire format:
//
//	command=fetch\n
//	[agent=<agent>]\n
//	[object-format=<algo>]\n
//	0001 (delimiter)
//	[want <oid>]\n
//	[want-ref <refname>]\n
//	[have <oid>]\n
//	[done]\n
//	[thin-pack]\n
//	[no-progress]\n
//	[include-tag]\n
//	[ofs-delta]\n
//	[shallow <oid>]\n
//	[deepen <depth>]\n
//	[deepen-since <timestamp>]\n
//	[deepen-not <ref>]\n
//	[deepen-relative]\n
//	[filter <spec>]\n
//	[sideband-all]\n
//	[packfile-uris <uri>...]\n
//	0000 (flush)
type V2FetchRequest struct {
	Wants    []plumbing.Hash
	WantRefs []string
	Haves    []plumbing.Hash
	Done     bool

	ThinPack   bool
	NoProgress bool
	IncludeTag bool
	OFSDelta   bool

	Shallows       []plumbing.Hash
	Depth          int
	DeepenSince    int64  // Unix timestamp; 0 means not set.
	DeepenNot      string // Ref name; empty means not set.
	DeepenRelative bool

	Filter Filter

	SidebandAll bool

	// ObjectFormat is the hash algorithm (e.g. "sha1", "sha256").
	// Empty means the server default (sha1).
	ObjectFormat string
}

// Encode writes the V2 fetch command request to w.
func (r *V2FetchRequest) Encode(w io.Writer) error {
	if _, err := pktline.Writeln(w, "command=fetch"); err != nil {
		return err
	}

	if _, err := pktline.Writef(w, "agent=%s\n", capability.DefaultAgent()); err != nil {
		return err
	}

	if r.ObjectFormat != "" {
		if _, err := pktline.Writef(w, "object-format=%s\n", r.ObjectFormat); err != nil {
			return err
		}
	}

	if err := pktline.WriteDelim(w); err != nil {
		return err
	}

	// Wants
	for _, want := range r.Wants {
		if _, err := pktline.Writef(w, "want %s\n", want); err != nil {
			return err
		}
	}

	for _, ref := range r.WantRefs {
		if _, err := pktline.Writef(w, "want-ref %s\n", ref); err != nil {
			return err
		}
	}

	// Haves
	for _, have := range r.Haves {
		if _, err := pktline.Writef(w, "have %s\n", have); err != nil {
			return err
		}
	}

	// Feature lines
	if r.ThinPack {
		if _, err := pktline.Writeln(w, "thin-pack"); err != nil {
			return err
		}
	}
	if r.NoProgress {
		if _, err := pktline.Writeln(w, "no-progress"); err != nil {
			return err
		}
	}
	if r.IncludeTag {
		if _, err := pktline.Writeln(w, "include-tag"); err != nil {
			return err
		}
	}
	if r.OFSDelta {
		if _, err := pktline.Writeln(w, "ofs-delta"); err != nil {
			return err
		}
	}
	if r.SidebandAll {
		if _, err := pktline.Writeln(w, "sideband-all"); err != nil {
			return err
		}
	}

	// Shallow/deepen
	for _, sh := range r.Shallows {
		if _, err := pktline.Writef(w, "shallow %s\n", sh); err != nil {
			return err
		}
	}
	if r.Depth > 0 {
		if _, err := pktline.Writef(w, "deepen %d\n", r.Depth); err != nil {
			return err
		}
	}
	if r.DeepenSince > 0 {
		if _, err := pktline.Writef(w, "deepen-since %d\n", r.DeepenSince); err != nil {
			return err
		}
	}
	if r.DeepenNot != "" {
		if _, err := pktline.Writef(w, "deepen-not %s\n", r.DeepenNot); err != nil {
			return err
		}
	}
	if r.DeepenRelative {
		if _, err := pktline.Writeln(w, "deepen-relative"); err != nil {
			return err
		}
	}

	// Filter
	if r.Filter != "" {
		if _, err := pktline.Writef(w, "filter %s\n", r.Filter); err != nil {
			return err
		}
	}

	// Done
	if r.Done {
		if _, err := pktline.Writeln(w, "done"); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}

// V2FetchResponse represents the response to a V2 fetch command.
//
// The response is made of labeled sections separated by delimiter
// packets (0001):
//
//	[acknowledgments\n
//	  ACK <oid>\n
//	  ...
//	  [ready\n | NAK\n]
//	0001]
//
//	[shallow-info\n
//	  [shallow <oid>\n]
//	  [unshallow <oid>\n]
//	  ...
//	0001]
//
//	[wanted-refs\n
//	  <oid> <refname>\n
//	  ...
//	0001]
//
//	[packfile\n
//	  <sideband-encoded packfile data>
//	0000]
type V2FetchResponse struct {
	// Acknowledgments from the server.
	ACKs []plumbing.Hash

	// Ready indicates the server has enough information to send the
	// packfile. If false after decoding, the client should send more
	// haves.
	Ready bool

	// ShallowUpdate contains shallow/unshallow information if present.
	ShallowUpdate *ShallowUpdate

	// WantedRefs maps requested ref names to their resolved OIDs.
	WantedRefs map[string]plumbing.Hash

	// Packfile is the reader for the (possibly sideband-encoded)
	// packfile data. It is non-nil only when the server sent the
	// packfile section. The caller is responsible for reading from it.
	// The reader provides the raw bytes after the "packfile\n" section
	// header — the caller must handle sideband demuxing.
	Packfile io.Reader
}

// NewV2FetchResponse creates a new, empty V2FetchResponse.
func NewV2FetchResponse() *V2FetchResponse {
	return &V2FetchResponse{
		WantedRefs: make(map[string]plumbing.Hash),
	}
}

// Decode reads a V2 fetch response from r. If the response contains a
// packfile section, resp.Packfile is set to r so the caller can read
// the remaining data (sideband-encoded pack data).
func (resp *V2FetchResponse) Decode(r io.Reader) error {
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return fmt.Errorf("reading V2 fetch response: %w", err)
		}

		if l == pktline.Flush {
			return nil
		}

		section := string(bytes.TrimSuffix(line, eol))

		switch section {
		case "acknowledgments":
			if err := resp.decodeAcknowledgments(r); err != nil {
				return fmt.Errorf("decoding acknowledgments: %w", err)
			}
		case "shallow-info":
			if err := resp.decodeShallowInfo(r); err != nil {
				return fmt.Errorf("decoding shallow-info: %w", err)
			}
		case "wanted-refs":
			if err := resp.decodeWantedRefs(r); err != nil {
				return fmt.Errorf("decoding wanted-refs: %w", err)
			}
		case "packfile":
			// The rest of the stream is the sideband-encoded packfile.
			// Hand it to the caller.
			resp.Packfile = r
			return nil
		default:
			// Skip unknown sections until delimiter or flush.
			if err := resp.skipSection(r); err != nil {
				return err
			}
		}
	}
}

func (resp *V2FetchResponse) decodeAcknowledgments(r io.Reader) error {
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return err
		}

		if l == pktline.Flush || l == pktline.Delim {
			return nil
		}

		text := string(bytes.TrimSuffix(line, eol))

		switch {
		case strings.HasPrefix(text, "ACK "):
			hash := plumbing.NewHash(strings.TrimPrefix(text, "ACK "))
			resp.ACKs = append(resp.ACKs, hash)
		case text == "ready":
			resp.Ready = true
		case text == "NAK":
			// No common objects.
		}
	}
}

func (resp *V2FetchResponse) decodeShallowInfo(r io.Reader) error {
	resp.ShallowUpdate = &ShallowUpdate{}

	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return err
		}

		if l == pktline.Flush || l == pktline.Delim {
			return nil
		}

		text := string(bytes.TrimSuffix(line, eol))

		switch {
		case strings.HasPrefix(text, "shallow "):
			hash := plumbing.NewHash(strings.TrimPrefix(text, "shallow "))
			resp.ShallowUpdate.Shallows = append(resp.ShallowUpdate.Shallows, hash)
		case strings.HasPrefix(text, "unshallow "):
			hash := plumbing.NewHash(strings.TrimPrefix(text, "unshallow "))
			resp.ShallowUpdate.Unshallows = append(resp.ShallowUpdate.Unshallows, hash)
		}
	}
}

func (resp *V2FetchResponse) decodeWantedRefs(r io.Reader) error {
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return err
		}

		if l == pktline.Flush || l == pktline.Delim {
			return nil
		}

		text := string(bytes.TrimSuffix(line, eol))

		// Format: <oid> <refname>
		oid, ref, ok := strings.Cut(text, " ")
		if !ok {
			return NewErrUnexpectedData("invalid wanted-refs line", []byte(text))
		}

		resp.WantedRefs[ref] = plumbing.NewHash(oid)
	}
}

func (resp *V2FetchResponse) skipSection(r io.Reader) error {
	for {
		l, _, err := pktline.ReadLine(r)
		if err != nil {
			return err
		}

		if l == pktline.Flush || l == pktline.Delim {
			return nil
		}
	}
}

// Encode writes the V2 fetch response to w. This is used server-side.
// Note: the packfile section must be written by the caller after Encode
// returns, since it involves streaming the pack data.
func (resp *V2FetchResponse) Encode(w io.Writer) error {
	// Acknowledgments section
	if len(resp.ACKs) > 0 || resp.Ready {
		if _, err := pktline.Writeln(w, "acknowledgments"); err != nil {
			return err
		}
		for _, ack := range resp.ACKs {
			if _, err := pktline.Writef(w, "ACK %s\n", ack); err != nil {
				return err
			}
		}
		if resp.Ready {
			if _, err := pktline.Writeln(w, "ready"); err != nil {
				return err
			}
		} else if len(resp.ACKs) == 0 {
			if _, err := pktline.Writeln(w, "NAK"); err != nil {
				return err
			}
		}
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
	}

	// Shallow-info section
	if resp.ShallowUpdate != nil &&
		(len(resp.ShallowUpdate.Shallows) > 0 || len(resp.ShallowUpdate.Unshallows) > 0) {
		if _, err := pktline.Writeln(w, "shallow-info"); err != nil {
			return err
		}
		for _, sh := range resp.ShallowUpdate.Shallows {
			if _, err := pktline.Writef(w, "shallow %s\n", sh); err != nil {
				return err
			}
		}
		for _, ush := range resp.ShallowUpdate.Unshallows {
			if _, err := pktline.Writef(w, "unshallow %s\n", ush); err != nil {
				return err
			}
		}
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
	}

	// Wanted-refs section
	if len(resp.WantedRefs) > 0 {
		if _, err := pktline.Writeln(w, "wanted-refs"); err != nil {
			return err
		}
		for ref, oid := range resp.WantedRefs {
			if _, err := pktline.Writef(w, "%s %s\n", oid, ref); err != nil {
				return err
			}
		}
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
	}

	// Packfile section header (caller writes pack data after this).
	if resp.Packfile != nil {
		if _, err := pktline.Writeln(w, "packfile"); err != nil {
			return err
		}
	}

	return nil
}

// ParseV2DeepenCommits parses a "deepen N" value from a V2 fetch argument.
func ParseV2DeepenCommits(s string) (int, error) {
	return strconv.Atoi(s)
}
