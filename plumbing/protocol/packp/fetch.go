package packp

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// FetchArgs represents the arguments for the v2 fetch command.
type FetchArgs struct {
	// Wants is the list of object IDs the client wants.
	Wants []plumbing.Hash
	// Haves is the list of object IDs the client already has.
	Haves []plumbing.Hash
	// Done indicates the client is done sending wants and haves.
	// If false, the client may send additional want/have lines
	// in subsequent request rounds (stateful transport only).
	Done bool
	// ThinPack requests a thin pack if the server supports it.
	ThinPack bool
	// NoProgress requests that the server suppress progress messages.
	NoProgress bool
	// IncludeTag requests that the server include tag objects.
	IncludeTag bool
	// OFSDelta requests that the server use OFS_DELTA objects.
	OFSDelta bool
	// Shallows is the list of shallow object IDs the client has.
	Shallows []plumbing.Hash
	// Deepen specifies the number of depth commits to fetch.
	Deepen int
	// DeepenRelative indicates that deepen is relative to the shallow boundary.
	DeepenRelative bool
	// DeepenSince specifies a time-based depth constraint.
	DeepenSince time.Time
	// DeepenNot specifies references to exclude from the shallow boundary.
	DeepenNot []string

	// Filter specifies a partial clone filter.
	Filter Filter
	// WaitForDone indicates that the client will wait for the server to send a
	// done acknowledgment before sending additional want/have lines.
	WaitForDone bool
}

// Encode writes the v2 fetch command arguments to a writer.
// Each argument is written as a separate pkt-line.
// The caller is responsible for writing the delim-pkt before and
// the flush-pkt after these arguments.
func (r *FetchArgs) Encode(w io.Writer) error {
	if len(r.Wants) == 0 {
		return fmt.Errorf("empty wants provided")
	}

	wants := append([]plumbing.Hash(nil), r.Wants...)
	plumbing.HashesSort(wants)
	for _, h := range wants {
		if _, err := pktline.Writef(w, "want %s\n", h); err != nil {
			return fmt.Errorf("encoding want %q: %s", h, err)
		}
	}

	haves := append([]plumbing.Hash(nil), r.Haves...)
	plumbing.HashesSort(haves)
	for _, h := range haves {
		if _, err := pktline.Writef(w, "have %s\n", h); err != nil {
			return fmt.Errorf("encoding have %q: %s", h, err)
		}
	}

	if r.Done {
		if _, err := pktline.WriteString(w, "done\n"); err != nil {
			return fmt.Errorf("encoding done: %s", err)
		}
	}

	if r.ThinPack {
		if _, err := pktline.WriteString(w, "thin-pack\n"); err != nil {
			return fmt.Errorf("encoding thin-pack: %s", err)
		}
	}

	if r.NoProgress {
		if _, err := pktline.WriteString(w, "no-progress\n"); err != nil {
			return fmt.Errorf("encoding no-progress: %s", err)
		}
	}

	if r.IncludeTag {
		if _, err := pktline.WriteString(w, "include-tag\n"); err != nil {
			return fmt.Errorf("encoding include-tag: %s", err)
		}
	}

	if r.OFSDelta {
		if _, err := pktline.WriteString(w, "ofs-delta\n"); err != nil {
			return fmt.Errorf("encoding ofs-delta: %s", err)
		}
	}

	shallows := append([]plumbing.Hash(nil), r.Shallows...)
	plumbing.HashesSort(shallows)
	for _, h := range shallows {
		if _, err := pktline.Writef(w, "shallow %s\n", h); err != nil {
			return fmt.Errorf("encoding shallow %q: %s", h, err)
		}
	}

	if r.Deepen > 0 {
		if _, err := pktline.Writef(w, "deepen %d\n", r.Deepen); err != nil {
			return fmt.Errorf("encoding deepen %d: %s", r.Deepen, err)
		}
	}

	if r.DeepenRelative {
		// deepen-relative is a flag: the depth is carried by the "deepen <n>"
		// line above. Matches git's fetch-pack.c (packet "deepen-relative\n").
		if _, err := pktline.WriteString(w, "deepen-relative\n"); err != nil {
			return fmt.Errorf("encoding deepen-relative: %s", err)
		}
	}

	if !r.DeepenSince.IsZero() {
		if _, err := pktline.Writef(w, "deepen-since %d\n", r.DeepenSince.UTC().Unix()); err != nil {
			return fmt.Errorf("encoding deepen-since %s: %s", r.DeepenSince, err)
		}
	}

	for _, ref := range r.DeepenNot {
		if _, err := pktline.Writef(w, "deepen-not %s\n", ref); err != nil {
			return fmt.Errorf("encoding deepen-not %s: %s", ref, err)
		}
	}

	if r.Filter != "" {
		if _, err := pktline.Writef(w, "filter %s\n", r.Filter); err != nil {
			return fmt.Errorf("encoding filter %s: %s", r.Filter, err)
		}
	}

	if r.WaitForDone {
		if _, err := pktline.WriteString(w, "wait-for-done\n"); err != nil {
			return fmt.Errorf("encoding wait-for-done: %s", err)
		}
	}

	return nil
}

// Decode reads v2 fetch command arguments from a reader until a flush-pkt
// is encountered. The caller is responsible for reading the delim-pkt
// and command header before calling Decode.
func (r *FetchArgs) Decode(rd io.Reader) error {
	for {
		l, pkt, err := pktline.ReadLine(rd)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if l == pktline.Flush || l == pktline.Delim {
			return nil
		}

		line := strings.TrimSpace(string(pkt))
		if len(line) == 0 {
			return nil
		}

		switch {
		case strings.HasPrefix(line, "want "):
			h, ok := plumbing.FromHex(line[5:])
			if !ok {
				return fmt.Errorf("malformed want hash: %q", line[5:])
			}
			r.Wants = append(r.Wants, h)

		case strings.HasPrefix(line, "have "):
			h, ok := plumbing.FromHex(line[5:])
			if !ok {
				return fmt.Errorf("malformed have hash: %q", line[5:])
			}
			r.Haves = append(r.Haves, h)

		case line == "done":
			r.Done = true

		case line == "thin-pack":
			r.ThinPack = true

		case line == "no-progress":
			r.NoProgress = true

		case line == "include-tag":
			r.IncludeTag = true

		case line == "ofs-delta":
			r.OFSDelta = true

		case strings.HasPrefix(line, "shallow "):
			h, ok := plumbing.FromHex(line[8:])
			if !ok {
				return fmt.Errorf("malformed shallow hash: %q", line[8:])
			}
			r.Shallows = append(r.Shallows, h)

		case line == "deepen-relative":
			r.DeepenRelative = true

		case strings.HasPrefix(line, "deepen-relative "):
			// Legacy/lenient: the depth belongs to "deepen <n>"; the argument
			// here is ignored. git only ever sends the bare flag.
			r.DeepenRelative = true

		case strings.HasPrefix(line, "deepen-since "):
			secs, e := strconv.ParseInt(line[13:], 10, 64)
			if e != nil {
				return fmt.Errorf("malformed deepen-since: %q", line)
			}
			r.DeepenSince = time.Unix(secs, 0).UTC()

		case strings.HasPrefix(line, "deepen-not "):
			r.DeepenNot = append(r.DeepenNot, line[11:])

		case strings.HasPrefix(line, "deepen "):
			n, e := strconv.Atoi(line[7:])
			if e != nil {
				return fmt.Errorf("malformed deepen: %q", line)
			}
			r.Deepen = n

		case strings.HasPrefix(line, "filter "):
			r.Filter = Filter(line[7:])

		case line == "wait-for-done":
			r.WaitForDone = true
		}
	}
}

// Acknowledgments represents the server response to a v2 fetch command's
// acknowledgments section. It is used by the transport layer to determine
// which objects the server has in common with the client.
type Acknowledgments struct {
	// ACKs is the list of common object IDs acknowledged by the server.
	// Empty list means the server found no common objects (NAK).
	ACKs []plumbing.Hash
	// Ready indicates the server is ready to send a packfile after the
	// acknowledgments section. For stream transports, ready is implied and
	// this field is always true.
	Ready bool
}

// ShallowInfo represents the server response to a v2 fetch command's
// shallow-info section. It is used by the transport layer to update the
// client's shallow boundary after a fetch.
type ShallowInfo struct {
	// Shallows is the list of shallow object IDs sent by the server.
	Shallows []plumbing.Hash
	// Unshallows is the list of object IDs that are no longer shallow.
	Unshallows []plumbing.Hash
}

// WantedRefs represents the server response to a v2 fetch command's
// wanted-refs section. It is used by the transport layer to determine which
// references the server wants the client to have.
type WantedRefs struct {
	// Refs is the list of references sent by the server.
	Refs []*plumbing.Reference
}

// PackfileURIs represents the server response to a v2 fetch command's
// packfile-uris section. It is used by the transport layer to determine which
// alternate URIs the server suggests for fetching the packfile.
type PackfileURIs struct {
	// URIs is the list of alternate URIs the server suggests for fetching the
	// packfile.
	URIs []string
}

// FetchOutput represents the server response to a v2 fetch command.
//
// The response has explicit sections separated by delim-pkt:
//
//	acknowledgments\n
//	ACK <oid>\n
//	ready\n
//	0001
//	shallow-info\n
//	shallow <oid>\n
//	0001
//	packfile\n
//	<sideband packfile data>
//	0000
//
// For HTTP, the transport layer consumes response-end (0002) after Decode returns.
type FetchOutput struct {
	// Acknowledgments indicates the server sent an acknowledgments section.
	Acknowledgments *Acknowledgments
	// ShallowInfo indicates the server sent a shallow-info section.
	ShallowInfo *ShallowInfo
	// WantedRefs indicates the server sent a wanted-refs section.
	WantedRefs *WantedRefs
	// PackfileURIs indicates the server sent a packfile-uris section.
	PackfileURIs *PackfileURIs
	// Packfile reports whether a packfile section follows the metadata
	// sections. When true, Decode leaves the reader positioned at the first
	// packfile pkt-line so the caller can stream it, and Encode writes the
	// "packfile" section header so the caller can write the packfile data.
	// When false, the response is a negotiation round
	// (acknowledgments flush-pkt) that carries no packfile.
	Packfile bool
}

// Decode reads the v2 fetch response from a reader. The response has
// explicit sections separated by delim-pkt:
//
//	acknowledgments\n
//	ACK <oid>\n
//	ready\n
//	0001
//	shallow-info\n
//	shallow <oid>\n
//	0001
//	packfile\n
//	<sideband packfile data>
//	0000
//
// A response is one of two shapes (gitprotocol-v2):
//
//	output = acknowledgments flush-pkt |
//	         [acknowledgments delim-pkt] [shallow-info delim-pkt]
//	         [wanted-refs delim-pkt] [packfile-uris delim-pkt]
//	         packfile flush-pkt
//
// When a metadata section ends with a flush-pkt (the first shape) the
// response is a negotiation round that carries no packfile, and Decode
// returns with Packfile set to false. When Decode reaches the "packfile"
// section header it sets Packfile to true and returns with the reader
// positioned at the first packfile pkt-line; Decode does not read the
// packfile data, leaving the caller to stream it (demultiplexing the
// sideband as needed).
//
// For HTTP, the transport layer consumes response-end (0002) after
// Decode returns.
func (r *FetchOutput) Decode(rd io.Reader) error {
	for {
		l, pkt, err := pktline.ReadLine(rd)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		// A flush-pkt at the top level ends a response with no packfile.
		if l == pktline.Flush || l == pktline.ResponseEnd {
			return nil
		}

		header := strings.TrimSpace(string(pkt))
		switch header {
		case "packfile":
			// Leave the reader positioned at the packfile data and let
			// the caller stream it. Decode never reads packfile bytes.
			r.Packfile = true
			return nil

		case "acknowledgments":
			r.Acknowledgments = &Acknowledgments{}
			term, err := r.decodeAcknowledgments(rd)
			if err != nil {
				return err
			}
			if term != pktline.Delim {
				return nil
			}

		case "shallow-info":
			r.ShallowInfo = &ShallowInfo{}
			term, err := r.decodeShallowInfo(rd)
			if err != nil {
				return err
			}
			if term != pktline.Delim {
				return nil
			}

		case "wanted-refs":
			r.WantedRefs = &WantedRefs{}
			term, err := r.decodeWantedRefs(rd)
			if err != nil {
				return err
			}
			if term != pktline.Delim {
				return nil
			}

		case "packfile-uris":
			r.PackfileURIs = &PackfileURIs{}
			term, err := r.decodePackfileURIs(rd)
			if err != nil {
				return err
			}
			if term != pktline.Delim {
				return nil
			}

		default:
			// Unknown section: skip its body to the next delimiter so the
			// following pkt-lines aren't mis-read as top-level section headers.
			term, err := skipSection(rd)
			if err != nil {
				return err
			}
			if term != pktline.Delim {
				return nil
			}
		}
	}
}

// skipSection consumes pkt-lines until a delim, flush, or response-end packet,
// returning which terminator was seen. It lets Decode tolerate unknown sections
// from a newer server without desynchronizing the stream.
func skipSection(rd io.Reader) (int, error) {
	for {
		l, _, err := pktline.ReadLine(rd)
		if err != nil {
			return 0, err
		}
		if l == pktline.Delim || l == pktline.Flush || l == pktline.ResponseEnd {
			return l, nil
		}
	}
}

// Encode writes the v2 fetch response to a writer.
//
// When Packfile is true, Encode writes the present metadata sections
// (acknowledgments, shallow-info, wanted-refs, packfile-uris), each
// terminated by a delim-pkt, followed by the "packfile" section header.
// The caller then streams the packfile data and writes the final
// flush-pkt.
//
// When Packfile is false, the response is a negotiation round: Encode
// writes the acknowledgments section terminated by a flush-pkt and writes
// nothing else. In that case the acknowledgments section must be present
// and must not be ready, and no other metadata sections may be set.
func (r *FetchOutput) Encode(w io.Writer) error {
	if !r.Packfile {
		if r.Acknowledgments == nil {
			return fmt.Errorf("fetch response without a packfile must carry acknowledgments")
		}
		if r.Acknowledgments.Ready {
			return fmt.Errorf("fetch response with ready must carry a packfile")
		}
		if r.ShallowInfo != nil || r.WantedRefs != nil || r.PackfileURIs != nil {
			return fmt.Errorf("fetch response without a packfile cannot carry metadata sections")
		}
		if _, err := pktline.WriteString(w, "acknowledgments\n"); err != nil {
			return err
		}
		if err := r.encodeAcknowledgments(w); err != nil {
			return err
		}
		return pktline.WriteFlush(w)
	}

	if r.Acknowledgments != nil {
		if _, err := pktline.WriteString(w, "acknowledgments\n"); err != nil {
			return err
		}
		if err := r.encodeAcknowledgments(w); err != nil {
			return err
		}
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
	}

	if r.ShallowInfo != nil {
		if _, err := pktline.WriteString(w, "shallow-info\n"); err != nil {
			return err
		}
		if err := r.encodeShallowInfo(w); err != nil {
			return err
		}
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
	}

	if r.WantedRefs != nil {
		if _, err := pktline.WriteString(w, "wanted-refs\n"); err != nil {
			return err
		}
		if err := r.encodeWantedRefs(w); err != nil {
			return err
		}
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
	}

	if r.PackfileURIs != nil {
		if _, err := pktline.WriteString(w, "packfile-uris\n"); err != nil {
			return err
		}
		if err := r.encodePackfileURIs(w); err != nil {
			return err
		}
		if err := pktline.WriteDelim(w); err != nil {
			return err
		}
	}

	// Packfile section header. The caller writes the packfile data and the
	// final flush-pkt after this.
	if _, err := pktline.WriteString(w, "packfile\n"); err != nil {
		return err
	}

	return nil
}

func (r *FetchOutput) decodeAcknowledgments(rd io.Reader) (int, error) {
	for {
		l, pkt, err := pktline.ReadLine(rd)
		if err != nil {
			return 0, err
		}

		if l == pktline.Delim || l == pktline.Flush || l == pktline.ResponseEnd {
			return l, nil
		}

		line := strings.TrimSpace(string(pkt))

		switch {
		case strings.HasPrefix(line, "ACK "):
			parts := strings.SplitN(line, " ", 2)
			if len(parts) < 2 {
				return 0, fmt.Errorf("malformed ACK line: %q", line)
			}
			h, ok := plumbing.FromHex(strings.TrimSpace(parts[1]))
			if !ok {
				return 0, fmt.Errorf("malformed ACK hash: %q", parts[1])
			}
			r.Acknowledgments.ACKs = append(r.Acknowledgments.ACKs, h)

		case line == "NAK":
			// NAK: no common objects

		case line == "ready":
			r.Acknowledgments.Ready = true
		}
	}
}

func (r *FetchOutput) decodeShallowInfo(rd io.Reader) (int, error) {
	for {
		l, pkt, err := pktline.ReadLine(rd)
		if err != nil {
			return 0, err
		}

		if l == pktline.Delim || l == pktline.Flush || l == pktline.ResponseEnd {
			return l, nil
		}

		line := strings.TrimSpace(string(pkt))

		switch {
		case strings.HasPrefix(line, "shallow "):
			h, ok := plumbing.FromHex(line[8:])
			if !ok {
				return 0, fmt.Errorf("malformed shallow hash: %q", line)
			}
			r.ShallowInfo.Shallows = append(r.ShallowInfo.Shallows, h)

		case strings.HasPrefix(line, "unshallow "):
			h, ok := plumbing.FromHex(line[10:])
			if !ok {
				return 0, fmt.Errorf("malformed unshallow hash: %q", line)
			}
			r.ShallowInfo.Unshallows = append(r.ShallowInfo.Unshallows, h)
		}
	}
}

func (r *FetchOutput) decodeWantedRefs(rd io.Reader) (int, error) {
	for {
		l, pkt, err := pktline.ReadLine(rd)
		if err != nil {
			return 0, err
		}

		if l == pktline.Delim || l == pktline.Flush || l == pktline.ResponseEnd {
			return l, nil
		}

		line := strings.TrimSpace(string(pkt))

		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			return 0, fmt.Errorf("malformed wanted-refs line: %q", line)
		}

		h, ok := plumbing.FromHex(parts[0])
		if !ok {
			return 0, fmt.Errorf("malformed wanted-refs hash: %q", parts[0])
		}

		r.WantedRefs.Refs = append(r.WantedRefs.Refs,
			plumbing.NewHashReference(plumbing.ReferenceName(parts[1]), h),
		)
	}
}

func (r *FetchOutput) decodePackfileURIs(rd io.Reader) (int, error) {
	for {
		l, pkt, err := pktline.ReadLine(rd)
		if err != nil {
			return 0, err
		}

		if l == pktline.Delim || l == pktline.Flush || l == pktline.ResponseEnd {
			return l, nil
		}

		line := strings.TrimSuffix(string(pkt), "\n")

		r.PackfileURIs.URIs = append(r.PackfileURIs.URIs, line)
	}
}

func (r *FetchOutput) encodeAcknowledgments(w io.Writer) error {
	for _, h := range r.Acknowledgments.ACKs {
		if _, err := pktline.Writef(w, "ACK %s\n", h); err != nil {
			return err
		}
	}
	if r.Acknowledgments.Ready {
		if _, err := pktline.WriteString(w, "ready\n"); err != nil {
			return err
		}
	}
	if len(r.Acknowledgments.ACKs) == 0 {
		if _, err := pktline.WriteString(w, "NAK\n"); err != nil {
			return err
		}
	}
	return nil
}

func (r *FetchOutput) encodeShallowInfo(w io.Writer) error {
	for _, h := range r.ShallowInfo.Shallows {
		if _, err := pktline.Writef(w, "shallow %s\n", h); err != nil {
			return err
		}
	}
	for _, h := range r.ShallowInfo.Unshallows {
		if _, err := pktline.Writef(w, "unshallow %s\n", h); err != nil {
			return err
		}
	}
	return nil
}

func (r *FetchOutput) encodeWantedRefs(w io.Writer) error {
	for _, ref := range r.WantedRefs.Refs {
		if _, err := pktline.Writef(w, "%s %s\n", ref.Hash(), ref.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (r *FetchOutput) encodePackfileURIs(w io.Writer) error {
	for _, uri := range r.PackfileURIs.URIs {
		if _, err := pktline.WriteString(w, uri+"\n"); err != nil {
			return err
		}
	}
	return nil
}
