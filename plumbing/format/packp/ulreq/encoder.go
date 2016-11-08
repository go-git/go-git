package ulreq

import (
	"fmt"
	"io"
	"sort"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/packp/pktline"
)

// An Encoder writes UlReq values to an output stream.
type Encoder struct {
	pe          *pktline.Encoder // where to write the encoded data
	data        *UlReq           // the data to encode
	sortedWants []string
	err         error // sticky error
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{
		pe: pktline.NewEncoder(w),
	}
}

// Encode writes the UlReq encoding of v to the stream.
//
// All the payloads will end with a newline character.  Wants and
// shallows are sorted alphabetically.  A depth of 0 means no depth
// request is sent.
func (e *Encoder) Encode(v *UlReq) error {
	if len(v.Wants) == 0 {
		return fmt.Errorf("empty wants provided")
	}

	e.data = v
	e.sortedWants = sortHashes(v.Wants)

	for state := encodeFirstWant; state != nil; {
		state = state(e)
	}

	return e.err
}

type encoderStateFn func(*Encoder) encoderStateFn

func sortHashes(list []plumbing.Hash) []string {
	sorted := make([]string, len(list))
	for i, hash := range list {
		sorted[i] = hash.String()
	}
	sort.Strings(sorted)

	return sorted
}

func encodeFirstWant(e *Encoder) encoderStateFn {
	var err error
	if e.data.Capabilities.IsEmpty() {
		err = e.pe.Encodef("want %s\n", e.sortedWants[0])
	} else {
		e.data.Capabilities.Sort()
		err = e.pe.Encodef(
			"want %s %s\n",
			e.sortedWants[0],
			e.data.Capabilities.String(),
		)
	}
	if err != nil {
		e.err = fmt.Errorf("encoding first want line: %s", err)
		return nil
	}

	return encodeAditionalWants
}

func encodeAditionalWants(e *Encoder) encoderStateFn {
	for _, w := range e.sortedWants[1:] {
		if err := e.pe.Encodef("want %s\n", w); err != nil {
			e.err = fmt.Errorf("encoding want %q: %s", w, err)
			return nil
		}
	}

	return encodeShallows
}

func encodeShallows(e *Encoder) encoderStateFn {
	sorted := sortHashes(e.data.Shallows)
	for _, s := range sorted {
		if err := e.pe.Encodef("shallow %s\n", s); err != nil {
			e.err = fmt.Errorf("encoding shallow %q: %s", s, err)
			return nil
		}
	}

	return encodeDepth
}

func encodeDepth(e *Encoder) encoderStateFn {
	switch depth := e.data.Depth.(type) {
	case DepthCommits:
		if depth != 0 {
			commits := int(depth)
			if err := e.pe.Encodef("deepen %d\n", commits); err != nil {
				e.err = fmt.Errorf("encoding depth %d: %s", depth, err)
				return nil
			}
		}
	case DepthSince:
		when := time.Time(depth).UTC()
		if err := e.pe.Encodef("deepen-since %d\n", when.Unix()); err != nil {
			e.err = fmt.Errorf("encoding depth %s: %s", when, err)
			return nil
		}
	case DepthReference:
		reference := string(depth)
		if err := e.pe.Encodef("deepen-not %s\n", reference); err != nil {
			e.err = fmt.Errorf("encoding depth %s: %s", reference, err)
			return nil
		}
	default:
		e.err = fmt.Errorf("unsupported depth type")
		return nil
	}

	return encodeFlush
}

func encodeFlush(e *Encoder) encoderStateFn {
	if err := e.pe.Flush(); err != nil {
		e.err = fmt.Errorf("encoding flush-pkt: %s", err)
		return nil
	}

	return nil
}
