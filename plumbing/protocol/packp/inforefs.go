package packp

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
)

// InfoRefs represents the information of the references advertised by an
// HTTP dumb server.
type InfoRefs struct {
	// References are the hash references, including peeled refs (whose
	// names end in ^{}). They are stored in the order received from the
	// server.
	References []*plumbing.Reference
}

// Decode decodes an InfoRefs from reader.
func (i *InfoRefs) Decode(r io.Reader) error {
	s := bufio.NewScanner(r)
	for s.Scan() {
		parts := strings.SplitN(s.Text(), "\t", 2)
		if len(parts) != 2 {
			continue
		}

		hash := plumbing.NewHash(parts[0])
		refname := parts[1]
		i.References = append(i.References, plumbing.NewHashReference(
			plumbing.ReferenceName(refname), hash,
		))
	}

	return s.Err()
}

// Encode encodes an InfoRefs to writer.
func (i *InfoRefs) Encode(w io.Writer) error {
	for _, ref := range i.References {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", ref.Hash().String(), ref.Name().String()); err != nil {
			return err
		}
	}

	return nil
}
