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

// NewAdvRefs returns a pointer to a new AdvRefs value, ready to be used.
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
		pe.EncodeString(c.String())
	}

	return pe.Flush()
}

func (a *AdvCaps) Decode(r io.Reader) error {
	s := pktline.NewScanner(r)

	// decode # service=<service>LF
	s.Scan()
	f := string(s.Bytes())
	if i := strings.Index(f, "service="); i < 0 {
		return fmt.Errorf("missing service indication")
	} else {
		a.Service = f[i+8:len(f)-1]
	}

	// scan flush
	s.Scan()
	if string(s.Bytes()) != "0000" {
		return fmt.Errorf("missing flush after service indication")
	}

	// now read capabilities
		
	return nil
}
