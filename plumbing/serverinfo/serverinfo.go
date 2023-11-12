package serverinfo

import (
	"fmt"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/internal/reference"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage"
)

// UpdateServerInfo updates the server info files in the repository.
//
// It generates a list of available refs for the repository.
// Used by git http transport (dumb), for more information refer to:
// https://git-scm.com/book/id/v2/Git-Internals-Transfer-Protocols#_the_dumb_protocol
func UpdateServerInfo(s storage.Storer, fs billy.Filesystem) error {
	pos, ok := s.(storer.PackedObjectStorer)
	if !ok {
		return git.ErrPackedObjectsNotSupported
	}

	infoRefs, err := fs.Create("info/refs")
	if err != nil {
		return err
	}

	defer infoRefs.Close()

	refsIter, err := s.IterReferences()
	if err != nil {
		return err
	}

	defer refsIter.Close()

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
			fmt.Fprintf(infoRefs, "%s\t%s\n", hash, name)
			if name.IsTag() {
				tag, err := object.GetTag(s, hash)
				if err == nil {
					fmt.Fprintf(infoRefs, "%s\t%s^{}\n", tag.Target, name)
				}
			}
		}
	}

	infoPacks, err := fs.Create("objects/info/packs")
	if err != nil {
		return err
	}

	defer infoPacks.Close()

	packs, err := pos.ObjectPacks()
	if err != nil {
		return err
	}

	for _, p := range packs {
		fmt.Fprintf(infoPacks, "P pack-%s.pack\n", p)
	}

	fmt.Fprintln(infoPacks)

	return nil
}
