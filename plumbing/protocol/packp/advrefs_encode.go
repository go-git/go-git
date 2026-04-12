package packp

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// Encode writes the AdvRefs encoding to a writer.
//
// All the payloads will end with a newline character. Capabilities,
// references and shallows are written in alphabetical order, except for
// peeled references that always follow their corresponding references.
func (a *AdvRefs) Encode(w io.Writer) error {
	e := newAdvRefsEncoder(w)
	return e.Encode(a)
}

type advRefsEncoder struct {
	data         *AdvRefs              // data to encode
	w            io.Writer             // where to write the encoded data
	firstRefName string                // reference name to encode in the first pkt-line (HEAD if present)
	firstRefHash plumbing.Hash         // hash referenced to encode in the first pkt-line (HEAD if present)
	sortedRefs   []*plumbing.Reference // sorted non-HEAD, non-peeled references
	err          error                 // sticky error
}

func newAdvRefsEncoder(w io.Writer) *advRefsEncoder {
	return &advRefsEncoder{
		w: w,
	}
}

func (e *advRefsEncoder) Encode(v *AdvRefs) error {
	e.data = v
	e.sortRefs()
	e.setFirstRef()

	for state := encodeFirstLine; state != nil; {
		state = state(e)
	}

	return e.err
}

// peeledToMap builds a map from reference name (without ^{} suffix) to
// the peeled hash, for only the peeled refs in the slice.
func peeledToMap(refs []*plumbing.Reference) map[string]plumbing.Hash {
	m := make(map[string]plumbing.Hash)
	for _, ref := range refs {
		name := ref.Name().String()
		if base, ok := strings.CutSuffix(name, "^{}"); ok {
			m[base] = ref.Hash()
		}
	}
	return m
}

// sortedNonPeeledRefs returns non-peeled, non-HEAD references sorted by name.
func sortedNonPeeledRefs(refs []*plumbing.Reference) []*plumbing.Reference {
	var out []*plumbing.Reference
	for _, ref := range refs {
		if ref.Name().IsPeeled() {
			continue
		}
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

func (e *advRefsEncoder) sortRefs() {
	e.sortedRefs = sortedNonPeeledRefs(e.data.References)
}

func (e *advRefsEncoder) setFirstRef() {
	if headRef, err := e.data.Head(); err == nil {
		e.firstRefName = headRef.Name().String()
		e.firstRefHash = headRef.Hash()
		return
	}

	if len(e.sortedRefs) > 0 {
		refName := e.sortedRefs[0]
		e.firstRefName = refName.Name().String()
		e.firstRefHash = refName.Hash()
	}
}

type encoderStateFn func(*advRefsEncoder) encoderStateFn

// Adds the first pkt-line payload: head hash, head ref and capabilities.
// If HEAD ref is not found, the first reference ordered in increasing order will be used.
// If there aren't HEAD neither refs, the first line will be "PKT-LINE(zero-id SP "capabilities^{}" NUL capability-list)".
// See: https://github.com/git/git/blob/master/Documentation/technical/pack-protocol.txt
// See: https://github.com/git/git/blob/master/Documentation/technical/protocol-common.txt
func encodeFirstLine(e *advRefsEncoder) encoderStateFn {
	const formatFirstLine = "%s %s\x00%s\n"
	var firstLine string
	capabilities := formatCaps(e.data.Capabilities)

	if e.firstRefName == "" {
		firstLine = fmt.Sprintf(formatFirstLine, plumbing.ZeroHash.String(), "capabilities^{}", capabilities)
	} else {
		firstLine = fmt.Sprintf(formatFirstLine, e.firstRefHash.String(), e.firstRefName, capabilities)
	}

	if _, e.err = pktline.WriteString(e.w, firstLine); e.err != nil {
		return nil
	}

	return encodeRefs
}

func formatCaps(c capability.List) string {
	return c.String()
}

// Adds the (sorted) refs: hash SP refname EOL
// and their peeled refs if any.
func encodeRefs(e *advRefsEncoder) encoderStateFn {
	// Build a map for fast peeled lookup by base name.
	peeled := peeledToMap(e.data.References)

	for _, ref := range e.sortedRefs {
		if ref.Name().String() == e.firstRefName {
			continue
		}

		name := ref.Name().String()
		if _, e.err = pktline.Writef(e.w, "%s %s\n", ref.Hash().String(), name); e.err != nil {
			return nil
		}

		if hash, ok := peeled[name]; ok {
			if _, e.err = pktline.Writef(e.w, "%s %s^{}\n", hash.String(), name); e.err != nil {
				return nil
			}
		}
	}

	return encodeShallow
}

// Adds the (sorted) shallows: "shallow" SP hash EOL
func encodeShallow(e *advRefsEncoder) encoderStateFn {
	sorted := sortShallows(e.data.Shallows)
	for _, hash := range sorted {
		if _, e.err = pktline.Writef(e.w, "shallow %s\n", hash); e.err != nil {
			return nil
		}
	}

	return encodeFlush
}

func sortShallows(c []plumbing.Hash) []string {
	ret := make([]string, 0, len(c))
	for _, h := range c {
		ret = append(ret, h.String())
	}
	sort.Strings(ret)

	return ret
}

func encodeFlush(e *advRefsEncoder) encoderStateFn {
	e.err = pktline.WriteFlush(e.w)
	return nil
}
