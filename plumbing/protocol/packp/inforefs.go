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
	// References are the hash references.
	References map[string]plumbing.Hash
	// Peeled are the peeled hash references.
	Peeled map[string]plumbing.Hash
}

func (i *InfoRefs) init() {
	if i.References == nil {
		i.References = make(map[string]plumbing.Hash)
	}
	if i.Peeled == nil {
		i.Peeled = make(map[string]plumbing.Hash)
	}
}

// Decode decodes an InfoRefs from reader.
func (i *InfoRefs) Decode(r io.Reader) error {
	i.init()
	s := bufio.NewScanner(r)
	for s.Scan() {
		parts := strings.SplitN(s.Text(), "\t", 2)
		if len(parts) != 2 {
			continue
		}

		hash := plumbing.NewHash(parts[0])
		refname := parts[1]
		if strings.HasSuffix(refname, string(peeled)) {
			i.Peeled[strings.TrimSuffix(refname, string(peeled))] = hash
		} else {
			i.References[refname] = hash
		}
	}

	return s.Err()
}

// Encode encodes an InfoRefs to writer.
func (i *InfoRefs) Encode(w io.Writer) error {
	sortedRefs := sortRefs(i.References)
	for _, ref := range sortedRefs {
		hash := i.References[ref]
		if _, err := fmt.Fprintf(w, "%s\t%s\n", hash.String(), ref); err != nil {
			return err
		}
		if peeled, ok := i.Peeled[ref]; ok {
			if _, err := fmt.Fprintf(w, "%s\t%s%s\n", peeled.String(), ref, peeled); err != nil {
				return err
			}
		}
	}
	return nil
}
