// Package advrefs implements encoding and decoding advertised-refs
// messages from a git-upload-pack command.
package advrefs

import (
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/packp"
)

const (
	hashSize = 40
	head     = "HEAD"
	noHead   = "capabilities^{}"
)

var (
	sp         = []byte(" ")
	null       = []byte("\x00")
	eol        = []byte("\n")
	peeled     = []byte("^{}")
	shallow    = []byte("shallow ")
	noHeadMark = []byte(" capabilities^{}\x00")
)

// AdvRefs values represent the information transmitted on an
// advertised-refs message.  Values from this type are not zero-value
// safe, use the New function instead.
//
// When using this messages over (smart) HTTP, you have to add a pktline
// before the whole thing with the following payload:
//
// '# service=$servicename" LF
//
// Moreover, some (all) git HTTP smart servers will send a flush-pkt
// just after the first pkt-line.
//
// To accomodate both situations, the Prefix field allow you to store
// any data you want to send before the actual pktlines.  It will also
// be filled up with whatever is found on the line.
type AdvRefs struct {
	Prefix       [][]byte // payloads of the prefix
	Head         *core.Hash
	Capabilities *packp.Capabilities
	References   map[string]core.Hash
	Peeled       map[string]core.Hash
	Shallows     []core.Hash
}

// New returns a pointer to a new AdvRefs value, ready to be used.
func New() *AdvRefs {
	return &AdvRefs{
		Prefix:       [][]byte{},
		Capabilities: packp.NewCapabilities(),
		References:   make(map[string]core.Hash),
		Peeled:       make(map[string]core.Hash),
		Shallows:     []core.Hash{},
	}
}
