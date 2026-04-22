package packp

import (
	"errors"
	"fmt"
	"io"
	"strings"

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

// Decode reads ls-refs arguments from a reader until a flush-pkt is encountered.
func (r *LsRefsArgs) Decode(rd io.Reader) error {
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
			return nil
		}

		switch {
		case line == "peel":
			r.Peel = true
		case line == "symrefs":
			r.Symrefs = true
		case line == "unborn":
			r.Unborn = true
		case strings.HasPrefix(line, "ref-prefix "):
			r.RefPrefixes = append(r.RefPrefixes, line[len("ref-prefix "):])
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

// Encode writes the ls-refs response lines as pkt-lines.
// The caller is responsible for writing the flush-pkt after these lines.
func (r *LsRefsOutput) Encode(w io.Writer) error {
	for _, ref := range r.References {
		name := ref.Name().String()
		if ref.Type() == plumbing.SymbolicReference {
			if _, err := pktline.Writef(w, "%s %s symref-target:%s\n",
				plumbing.ZeroHash.String(), name, ref.Target()); err != nil {
				return err
			}
			continue
		}

		baseName := name
		isPeeled := ref.Name().IsPeeled()
		if isPeeled {
			baseName = strings.TrimSuffix(name, "^{}")
		}

		if isPeeled {
			// Peeled ref: <peeled-oid> SP <base-refname>^{} SP peeled:<base-oid>
			// We need the base hash to produce the peeled line. Since we only
			// have the peeled hash, we encode it as:
			// <peeled-oid> SP <base-refname>^{}
			if _, err := pktline.Writef(w, "%s %s^{}\n", ref.Hash().String(), baseName); err != nil {
				return err
			}
		} else {
			if _, err := pktline.Writef(w, "%s %s\n", ref.Hash().String(), name); err != nil {
				return err
			}
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
			return nil
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
	// Split on SP: [oid-or-unborn, refname, attr1, attr2, ...]
	parts := strings.Split(line, " ")
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
			h, ok := plumbing.FromHex(attr[len("peeled:"):])
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
		if symrefTarget != "" {
			refs = append(refs, plumbing.NewSymbolicReference(refName, symrefTarget))
		}
		return refs, nil
	}

	// Regular hash ref
	hash := plumbing.NewHash(oidStr)

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
