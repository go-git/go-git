package packp

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// LsRefsArgs represents the arguments for the v2 ls-refs command.
// It is encoded as the command-specific arguments and a flush-pkt in a v2
// command request.
type LsRefsArgs struct {
	Peel        bool
	Symrefs     bool
	Unborn      bool
	RefPrefixes []string
}

// Encode writes the ls-refs arguments to a writer. Each argument is
// written as a separate pkt-line. The caller is responsible for writing
// the delim-pkt before and the flush-pkt after these arguments.
func (r *LsRefsArgs) Encode(w io.Writer) error {
	// Validate every ref-prefix before writing anything, so an invalid prefix
	// can never leave a partially-written arguments section on the stream
	// (Encode is all-or-nothing on a validation error).
	for _, p := range r.RefPrefixes {
		if err := validateRefPrefix(p); err != nil {
			return err
		}
	}

	if r.Peel {
		if _, err := pktline.WriteString(w, "peel\n"); err != nil {
			return err
		}
	}
	if r.Symrefs {
		if _, err := pktline.WriteString(w, "symrefs\n"); err != nil {
			return err
		}
	}
	if r.Unborn {
		if _, err := pktline.WriteString(w, "unborn\n"); err != nil {
			return err
		}
	}
	for _, p := range r.RefPrefixes {
		if _, err := pktline.Writef(w, "ref-prefix %s\n", p); err != nil {
			return err
		}
	}
	return nil
}

// validateRefPrefix rejects a ref-prefix that cannot be safely framed as a
// "ref-prefix <p>" pkt-line. An empty prefix would emit a stray "ref-prefix "
// argument, and whitespace or control bytes (notably LF and NUL) would break
// the pkt-line framing or let a caller inject extra lines. No valid Git
// reference contains such characters, so this only rejects malformed input.
func validateRefPrefix(p string) error {
	if p == "" {
		return fmt.Errorf("invalid ref-prefix: empty")
	}
	for _, c := range p {
		if c == 0 || unicode.IsControl(c) || unicode.IsSpace(c) {
			return fmt.Errorf("invalid ref-prefix %q: contains whitespace or control character", p)
		}
	}
	return nil
}

// tooManyRefPrefixes mirrors ls-refs.c TOO_MANY_PREFIXES: past this many
// ref-prefix arguments, upstream clears the list and advertises every ref, both
// to bound memory and because prefix filtering stops paying off.
const tooManyRefPrefixes = 65536

// Decode reads ls-refs arguments from a reader until a flush-pkt is encountered.
func (r *LsRefsArgs) Decode(rd io.Reader) error {
	tooMany := false
	for {
		l, pkt, err := pktline.ReadLine(rd)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if l == pktline.Flush {
			return nil
		}

		line := strings.TrimSuffix(string(pkt), "\n")

		if len(line) == 0 {
			continue
		}

		switch {
		case line == "peel":
			r.Peel = true
		case line == "symrefs":
			r.Symrefs = true
		case line == "unborn":
			r.Unborn = true
		case strings.HasPrefix(line, "ref-prefix "):
			if tooMany {
				continue
			}
			r.RefPrefixes = append(r.RefPrefixes, line[len("ref-prefix "):])
			if len(r.RefPrefixes) >= tooManyRefPrefixes {
				// Too many prefixes: drop them and advertise every ref, as
				// upstream ls-refs.c does, instead of growing without bound.
				r.RefPrefixes = nil
				tooMany = true
			}
		}
	}
}

// LsRefsOutput represents the server response to an ls-refs command.
//
// Each ref line has the format:
//
//	<oid> SP <refname> [SP symref-target:<target>] [SP peeled:<oid>]
//
// or for unborn refs:
//
//	unborn SP <refname> SP symref-target:<target>
//
// The response ends with a flush-pkt. For HTTP, response-end (0002) is
// consumed by the transport layer and not seen by Decode.
type LsRefsOutput struct {
	References []*plumbing.Reference
}

// Encode writes the ls-refs response lines as pkt-lines following the v2
// grammar: "<oid> SP <refname> [SP symref-target:<target>] [SP peeled:<oid>]",
// or "unborn SP <refname> SP symref-target:<target>" for an unborn HEAD. Peeled
// "^{}" entries are folded into their base ref's line as a peeled attribute, and
// a symbolic ref carries the resolved oid of its target when present. The caller
// is responsible for writing the flush-pkt after these lines.
func (r *LsRefsOutput) Encode(w io.Writer) error {
	hashByName := make(map[string]plumbing.Hash, len(r.References))
	for _, ref := range r.References {
		if ref.Type() == plumbing.HashReference {
			hashByName[ref.Name().String()] = ref.Hash()
		}
	}

	for _, ref := range r.References {
		name := ref.Name().String()

		// Peeled entries are folded into their base ref's line below.
		if ref.Name().IsPeeled() {
			continue
		}

		if ref.Type() == plumbing.SymbolicReference {
			oid := "unborn"
			if h, ok := hashByName[ref.Target().String()]; ok && !h.IsZero() {
				oid = h.String()
			}
			if _, err := pktline.Writef(w, "%s %s symref-target:%s\n", oid, name, ref.Target()); err != nil {
				return err
			}
			continue
		}

		line := fmt.Sprintf("%s %s", ref.Hash(), name)
		if peeled, ok := hashByName[name+"^{}"]; ok {
			line += " peeled:" + peeled.String()
		}
		if _, err := pktline.Writef(w, "%s\n", line); err != nil {
			return err
		}
	}

	return nil
}

// Decode reads ref lines until a flush-pkt.
func (r *LsRefsOutput) Decode(rd io.Reader) error {
	for {
		l, pkt, err := pktline.ReadLine(rd)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if l == pktline.Flush {
			return nil
		}

		line := strings.TrimSuffix(string(pkt), "\n")

		if len(line) == 0 {
			continue
		}

		refs, err := parseLsRefsLine(line)
		if err != nil {
			return err
		}
		r.References = append(r.References, refs...)
	}
}

// parseLsRefsLine parses a single ref line from ls-refs output.
// Format: <oid-or-unborn> SP <refname> [SP <attr>...] LF
// Returns one or two references (base + peeled if the peeled attribute is present).
func parseLsRefsLine(line string) ([]*plumbing.Reference, error) {
	// Fields tolerates the SP-separated grammar without producing empty tokens
	// on repeated spaces: [oid-or-unborn, refname, attr1, attr2, ...].
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed ref line: %q", line)
	}

	oidStr := parts[0]
	refName := plumbing.ReferenceName(parts[1])

	var symrefTarget plumbing.ReferenceName
	var peeledHash plumbing.Hash
	hasPeeled := false

	for _, attr := range parts[2:] {
		if strings.HasPrefix(attr, "symref-target:") {
			symrefTarget = plumbing.ReferenceName(attr[len("symref-target:"):])
		} else if strings.HasPrefix(attr, "peeled:") {
			h, ok := parseFullHash(attr[len("peeled:"):])
			if !ok {
				return nil, fmt.Errorf("malformed peeled hash: %q", attr)
			}
			peeledHash = h
			hasPeeled = true
		}
	}

	var refs []*plumbing.Reference

	// Handle unborn refs
	if oidStr == "unborn" {
		if symrefTarget == "" {
			return nil, fmt.Errorf("malformed unborn ref line, missing symref-target: %q", line)
		}
		refs = append(refs, plumbing.NewSymbolicReference(refName, symrefTarget))
		return refs, nil
	}

	// Regular hash ref
	hash, ok := parseFullHash(oidStr)
	if !ok {
		return nil, fmt.Errorf("malformed object id: %q", oidStr)
	}

	if symrefTarget != "" {
		refs = append(refs, plumbing.NewSymbolicReference(refName, symrefTarget))
	} else {
		refs = append(refs, plumbing.NewHashReference(refName, hash))
	}

	// If "peeled:" attribute is present, add the peeled ref as a separate entry
	if hasPeeled {
		refs = append(refs, plumbing.NewHashReference(
			plumbing.ReferenceName(refName.String()+"^{}"),
			peeledHash,
		))
	}

	return refs, nil
}

// parseFullHash strictly parses a full-length SHA-1 or SHA-256 object id in hex
// form. Object ids on the wire are always full length, so unlike
// plumbing.FromHex (which zero-pads shorter input as a partial SHA-1) it rejects
// anything that is not exactly an object-id length, refusing malformed input.
func parseFullHash(s string) (plumbing.Hash, bool) {
	if !plumbing.IsHash(s) {
		return plumbing.ZeroHash, false
	}
	return plumbing.FromHex(s)
}
