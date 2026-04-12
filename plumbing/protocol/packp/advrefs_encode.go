package packp

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// Encode writes the AdvRefs encoding to a writer.
//
// All the payloads will end with a newline character. Capabilities,
// references and shallows are written in alphabetical order, except for
// peeled references that always follow their corresponding references.
func (a *AdvRefs) Encode(w io.Writer) error {
	// Find HEAD or use first ref
	firstName, firstHash := a.firstRef()

	// Write first line: hash SP refname NUL capabilities
	caps := a.Capabilities.String()
	if firstName == "" {
		// No refs: zero-id capabilities^{}
		firstLine := fmt.Sprintf("%s %s\x00%s\n",
			plumbing.ZeroHash.String(), "capabilities^{}", caps)
		if _, err := pktline.WriteString(w, firstLine); err != nil {
			return err
		}
	} else {
		firstLine := fmt.Sprintf("%s %s\x00%s\n",
			firstHash.String(), firstName, caps)
		if _, err := pktline.WriteString(w, firstLine); err != nil {
			return err
		}
	}

	// Build peeled map
	peeled := make(map[string]plumbing.Hash)
	for _, ref := range a.References {
		name := ref.Name().String()
		if base, ok := strings.CutSuffix(name, "^{}"); ok {
			peeled[base] = ref.Hash()
		}
	}

	// Sort non-peeled refs (excluding HEAD which was already written)
	sorted := make([]*plumbing.Reference, 0, len(a.References))
	for _, ref := range a.References {
		if ref.Name().IsPeeled() || ref.Name().String() == firstName {
			continue
		}
		sorted = append(sorted, ref)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name() < sorted[j].Name()
	})

	// Write refs and their peeled versions
	for _, ref := range sorted {
		name := ref.Name().String()
		if _, err := pktline.Writef(w, "%s %s\n", ref.Hash().String(), name); err != nil {
			return err
		}
		if hash, ok := peeled[name]; ok {
			if _, err := pktline.Writef(w, "%s %s^{}\n", hash.String(), name); err != nil {
				return err
			}
		}
	}

	// Write shallows
	if len(a.Shallows) > 0 {
		shallowStrs := make([]string, len(a.Shallows))
		for i, h := range a.Shallows {
			shallowStrs[i] = h.String()
		}
		sort.Strings(shallowStrs)
		for _, h := range shallowStrs {
			if _, err := pktline.Writef(w, "shallow %s\n", h); err != nil {
				return err
			}
		}
	}

	return pktline.WriteFlush(w)
}

// firstRef returns the reference to use as the first line (HEAD or first available).
func (a *AdvRefs) firstRef() (string, plumbing.Hash) {
	for _, ref := range a.References {
		if ref.Name().IsPeeled() {
			continue
		}
		if ref.Name() == plumbing.HEAD {
			return ref.Name().String(), ref.Hash()
		}
	}
	for _, ref := range a.References {
		if ref.Name().IsPeeled() {
			continue
		}
		return ref.Name().String(), ref.Hash()
	}
	return "", plumbing.ZeroHash
}
