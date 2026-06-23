package packp

import (
	"errors"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// LsRefsRequest is the client request for the protocol v2 "ls-refs"
// command, used to discover the references advertised by a server.
type LsRefsRequest struct {
	// Capabilities are pre-formatted capability lines sent with every v2
	// command request, such as "agent=git/2.40.1" and "object-format=sha1".
	Capabilities []string
	// Symrefs requests that symbolic references report their target via a
	// "symref-target:" attribute.
	Symrefs bool
	// Peel requests that annotated tags report the object they peel to via
	// a "peeled:" attribute.
	Peel bool
	// Unborn requests that an unborn HEAD be reported.
	Unborn bool
	// RefPrefixes restricts the advertisement to references matching one of
	// the given prefixes. An empty slice advertises all references.
	RefPrefixes []string
}

// Encode writes the ls-refs command request to w.
func (r *LsRefsRequest) Encode(w io.Writer) error {
	if err := writeCommand(w, "ls-refs", r.Capabilities, func(w io.Writer) error {
		if r.Peel {
			if _, err := pktline.Writeln(w, "peel"); err != nil {
				return err
			}
		}
		if r.Symrefs {
			if _, err := pktline.Writeln(w, "symrefs"); err != nil {
				return err
			}
		}
		if r.Unborn {
			if _, err := pktline.Writeln(w, "unborn"); err != nil {
				return err
			}
		}
		for _, prefix := range r.RefPrefixes {
			if _, err := pktline.Writeln(w, "ref-prefix "+prefix); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// LsRefsResponse is the server's reply to an ls-refs command: the list of
// advertised references with symbolic and peeled targets resolved inline.
type LsRefsResponse struct {
	References []*plumbing.Reference
}

// Decode reads an ls-refs response from r until the terminating flush packet.
func (r *LsRefsResponse) Decode(reader io.Reader) error {
	for {
		l, line, err := pktline.ReadLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if l == pktline.Flush {
			break
		}

		if err := r.decodeRef(strings.TrimRight(string(line), "\n")); err != nil {
			return err
		}
	}
	return nil
}

func (r *LsRefsResponse) decodeRef(line string) error {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return errors.New("malformed ls-refs response line: " + line)
	}

	oid, name := fields[0], plumbing.ReferenceName(fields[1])

	var symrefTarget, peeled string
	for _, attr := range fields[2:] {
		switch {
		case strings.HasPrefix(attr, "symref-target:"):
			symrefTarget = strings.TrimPrefix(attr, "symref-target:")
		case strings.HasPrefix(attr, "peeled:"):
			peeled = strings.TrimPrefix(attr, "peeled:")
		}
	}

	if symrefTarget != "" {
		r.References = append(r.References,
			plumbing.NewSymbolicReference(name, plumbing.ReferenceName(symrefTarget)))
	} else if oid != "unborn" {
		r.References = append(r.References,
			plumbing.NewHashReference(name, plumbing.NewHash(oid)))
	}

	if peeled != "" {
		r.References = append(r.References,
			plumbing.NewHashReference(name+"^{}", plumbing.NewHash(peeled)))
	}

	return nil
}
