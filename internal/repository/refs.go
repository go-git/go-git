// Package repository provides internal utilities for repository operations.
package repository

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/internal/reference"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
)

// ExpandRef resolves a reference name using git ref parsing rules.
func ExpandRef(s storer.ReferenceStorer, ref plumbing.ReferenceName) (*plumbing.Reference, error) {
	// For improving troubleshooting, this preserves the error for the provided `ref`,
	// and returns the error for that specific ref in case all parse rules fails.
	var ret error
	for _, rule := range plumbing.RefRevParseRules {
		resolvedRef, err := storer.ResolveReference(s, plumbing.ReferenceName(fmt.Sprintf(rule, ref)))

		if err == nil {
			return resolvedRef, nil
		} else if ret == nil {
			ret = err
		}
	}

	return nil, ret
}

// WriteInfoRefs writes the info/refs file to the given writer.
// It generates a list of available refs for the repository.
// Used by git http transport (dumb), for more information refer to:
// https://git-scm.com/book/id/v2/Git-Internals-Transfer-Protocols#_the_dumb_protocol
func WriteInfoRefs(w io.Writer, s storage.Storer) error {
	refsIter, err := s.IterReferences()
	if err != nil {
		return err
	}

	var refs []*plumbing.Reference
	if err := refsIter.ForEach(func(ref *plumbing.Reference) error {
		refs = append(refs, ref)
		return nil
	}); err != nil {
		return err
	}

	reference.Sort(refs)
	for _, ref := range refs {
		name := ref.Name()
		hash := ref.Hash()
		switch ref.Type() {
		case plumbing.SymbolicReference:
			if name == plumbing.HEAD {
				continue
			}
			ref, err := s.Reference(ref.Target())
			if err != nil {
				return err
			}

			hash = ref.Hash()
			fallthrough
		case plumbing.HashReference:
			if _, err := fmt.Fprintf(w, "%s\t%s\n", hash, name); err != nil {
				return fmt.Errorf("writing info reference: %w", err)
			}
			if name.IsTag() {
				tag, err := object.GetTag(s, hash)
				if err == nil {
					if _, err := fmt.Fprintf(w, "%s\t%s^{}\n", tag.Target, name); err != nil {
						return fmt.Errorf("writing info tag reference: %w", err)
					}
				}
			}
		}
	}

	return nil
}

// WriteObjectsInfoPacks writes objects/info/packs for dumb transports. Storers
// that implement [storer.ObjectPackNamer] supply exact physical basenames.
// Other storers use canonical pack-<hash>.pack names.
func WriteObjectsInfoPacks(w io.Writer, s storer.PackedObjectStorer) error {
	names, err := objectPackNames(s)
	if err != nil {
		return err
	}

	for _, name := range names {
		if _, err := fmt.Fprintf(w, "P %s\n", name); err != nil {
			return fmt.Errorf("writing pack line reference: %w", err)
		}
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("writing pack line final newline: %w", err)
	}
	return nil
}

func objectPackNames(s storer.PackedObjectStorer) ([]string, error) {
	if namer, ok := s.(storer.ObjectPackNamer); ok {
		return namer.ObjectPackNames()
	}

	packs, err := s.ObjectPacks()
	if err != nil {
		return nil, err
	}

	names := make([]string, len(packs))
	for i, pack := range packs {
		names[i] = fmt.Sprintf("pack-%s.pack", pack)
	}
	return names, nil
}
