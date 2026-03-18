package packp

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

// LsRefsRequest represents a V2 ls-refs command request.
//
// Wire format:
//
//	command=ls-refs\n
//	[agent=<agent>]\n
//	0001 (delimiter)
//	[peel]\n
//	[symrefs]\n
//	[unborn]\n
//	[ref-prefix <prefix>]\n
//	...
//	0000 (flush)
type LsRefsRequest struct {
	// RefPrefixes limits the refs returned to those matching one of the
	// given prefixes. If empty, all refs are returned.
	RefPrefixes []string

	// IncludeSymRefs requests that symref targets be included in the response.
	IncludeSymRefs bool

	// IncludePeeled requests that peeled values be included for tags.
	IncludePeeled bool

	// IncludeUnborn requests that the server report unborn HEAD with its
	// symref target even if HEAD is not yet born.
	IncludeUnborn bool
}

// Encode writes the ls-refs command request to w in V2 wire format.
func (r *LsRefsRequest) Encode(w io.Writer) error {
	if _, err := pktline.Writeln(w, "command=ls-refs"); err != nil {
		return err
	}

	if _, err := pktline.Writef(w, "agent=%s\n", capability.DefaultAgent()); err != nil {
		return err
	}

	if err := pktline.WriteDelim(w); err != nil {
		return err
	}

	if r.IncludePeeled {
		if _, err := pktline.Writeln(w, "peel"); err != nil {
			return err
		}
	}

	if r.IncludeSymRefs {
		if _, err := pktline.Writeln(w, "symrefs"); err != nil {
			return err
		}
	}

	if r.IncludeUnborn {
		if _, err := pktline.Writeln(w, "unborn"); err != nil {
			return err
		}
	}

	for _, prefix := range r.RefPrefixes {
		if _, err := pktline.Writef(w, "ref-prefix %s\n", prefix); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}

// LsRefsResponse represents the response to a V2 ls-refs command.
//
// Wire format:
//
//	<oid> <refname>\n
//	<oid> <refname> symref-target:<target>\n
//	<oid> <refname> peeled:<peeled-oid>\n
//	...
//	0000 (flush)
type LsRefsResponse struct {
	// References is the list of references returned by the server.
	References []*plumbing.Reference

	// Peeled maps ref names to their peeled (dereferenced) OIDs.
	Peeled map[string]plumbing.Hash
}

// NewLsRefsResponse creates a new, empty LsRefsResponse.
func NewLsRefsResponse() *LsRefsResponse {
	return &LsRefsResponse{
		Peeled: make(map[string]plumbing.Hash),
	}
}

// Decode reads a ls-refs response from r.
func (resp *LsRefsResponse) Decode(r io.Reader) error {
	for {
		l, line, err := pktline.ReadLine(r)
		if err != nil {
			return fmt.Errorf("reading ls-refs response: %w", err)
		}

		if l == pktline.Flush {
			return nil
		}

		text := string(bytes.TrimSuffix(line, eol))
		if err := resp.decodeLine(text); err != nil {
			return err
		}
	}
}

func (resp *LsRefsResponse) decodeLine(line string) error {
	// Format: <oid> <refname>[ <attribute>...]
	// Attributes: symref-target:<target>, peeled:<oid>
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return NewErrUnexpectedData("invalid ls-refs line", []byte(line))
	}

	oid := plumbing.NewHash(parts[0])
	refName := plumbing.ReferenceName(parts[1])

	var ref *plumbing.Reference

	// Parse optional attributes.
	if len(parts) == 3 {
		attrs := parts[2]
		for attr := range strings.SplitSeq(attrs, " ") {
			switch {
			case strings.HasPrefix(attr, "symref-target:"):
				target := plumbing.ReferenceName(strings.TrimPrefix(attr, "symref-target:"))
				ref = plumbing.NewSymbolicReference(refName, target)
			case strings.HasPrefix(attr, "peeled:"):
				peeledOID := plumbing.NewHash(strings.TrimPrefix(attr, "peeled:"))
				resp.Peeled[refName.String()] = peeledOID
			}
		}
	}

	if ref == nil {
		ref = plumbing.NewHashReference(refName, oid)
	}

	resp.References = append(resp.References, ref)
	return nil
}

// Encode writes the ls-refs response to w.
func (resp *LsRefsResponse) Encode(w io.Writer) error {
	for _, ref := range resp.References {
		var line string
		switch ref.Type() {
		case plumbing.SymbolicReference:
			// For symrefs we need the resolved hash. Look for it in
			// references or use zero hash.
			hash := ref.Hash()
			line = fmt.Sprintf("%s %s symref-target:%s", hash, ref.Name(), ref.Target())
		default:
			line = fmt.Sprintf("%s %s", ref.Hash(), ref.Name())
		}

		if peeledHash, ok := resp.Peeled[ref.Name().String()]; ok {
			line += fmt.Sprintf(" peeled:%s", peeledHash)
		}

		if _, err := pktline.Writeln(w, line); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}
