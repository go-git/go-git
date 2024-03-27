package packp

import (
	"fmt"
	"io"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
)

// AdvCaps values represent the information transmitted on an the first v2
// message.  Values from this type are not zero-value
// safe, use the New function instead.
type AdvCaps struct {
	// Service represents the requested service.
	Service string
	// Capabilities are the capabilities.
	Capabilities *capability.List
}

// NewAdvCaps creates a new AdvCaps object, ready to be used.
func NewAdvCaps() *AdvCaps {
	return &AdvCaps{
		Capabilities: capability.NewList(),
	}
}

// IsEmpty returns true if doesn't contain any capability.
func (a *AdvCaps) IsEmpty() bool {
	return a.Capabilities.IsEmpty()
}

func (a *AdvCaps) Encode(w io.Writer) error {
	pe := pktline.NewEncoder(w)
	pe.EncodeString("# service=" + a.Service + "\n")
	pe.Flush()
	pe.EncodeString("version 2\n")

	for _, c := range a.Capabilities.All() {
		vals := a.Capabilities.Get(c)
		if len(vals) > 0 {
			pe.EncodeString(c.String() + "=" + strings.Join(vals, " ") + "\n")
		} else {
			pe.EncodeString(c.String() + "\n")
		}
	}

	return pe.Flush()
}

func (a *AdvCaps) Decode(r io.Reader) error {
	s := pktline.NewScanner(r)

	// decode # SP service=<service> LF
	s.Scan()
	f := string(s.Bytes())
	if i := strings.Index(f, "service="); i < 0 {
		return fmt.Errorf("missing service indication")
	}
	
	a.Service = f[i+8 : len(f)-1]

	// scan flush
	s.Scan()
	if !isFlush(s.Bytes()) {
		return fmt.Errorf("missing flush after service indication")
	}

	// now version LF
	s.Scan()
	if string(s.Bytes()) != "version 2\n" {
		return fmt.Errorf("missing version after flush")
	}

	// now read capabilities
	for s.Scan(); !isFlush(s.Bytes()); {
		if sp := strings.Split(string(s.Bytes()), "="); len(sp) == 2 {
			a.Capabilities.Add(capability.Capability((sp[0])))
		} else {
			a.Capabilities.Add(
				capability.Capability(sp[0]),
				strings.Split(sp[1], " ")...,
			)
		}
	}

	// read final flush
	s.Scan()
	if !isFlush(s.Bytes()) {
		return fmt.Errorf("missing flush after capability")
	}

	return nil
}
