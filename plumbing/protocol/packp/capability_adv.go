package packp

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

// CapabilityAdv represents a protocol v2 server capability advertisement.
// It includes the version line and the capability lines that follow it.
//
// In protocol v2, the server sends:
//
//	version 2\n
//	agent=git/2.45.0\n
//	ls-refs=unborn\n
//	fetch=shallow wait-for-done filter\n
//	0000
//
// Capabilities are one per line in "key" or "key=value" format,
// terminated by a flush packet. This differs from v0/v1 where
// capabilities are space-separated after a NUL byte on the first ref line.
type CapabilityAdv struct {
	// Version is the protocol version. Decode sets this to V2.
	// Encode writes the version line when Version is V2.
	Version protocol.Version
	// Capabilities is the parsed list of server capabilities.
	Capabilities capability.List
}

// Decode reads a v2 capability advertisement from a pkt-line stream.
// It expects the stream to start with the "version 2\n" line,
// followed by capability lines (one per line), terminated by a flush packet.
func (ca *CapabilityAdv) Decode(r io.Reader) error {
	// Read version line first.
	l, line, err := pktline.ReadLine(r)
	if err != nil {
		return err
	}
	if l < 4 || line == nil {
		return errInvalidVersionLine
	}

	line = bytes.TrimSuffix(line, []byte("\n"))
	if !bytes.HasPrefix(line, []byte("version ")) {
		return errInvalidVersionLine
	}

	v, err := protocol.Parse(string(line[8:]))
	if err != nil {
		return err
	}

	if v != protocol.V2 {
		return fmt.Errorf("unsupported protocol version in capability advertisement: %s", v)
	}

	ca.Version = v

	// Read capability lines until flush.
	length, err := DecodeListV2(r, &ca.Capabilities)
	if err != nil {
		return fmt.Errorf("decoding capability list: %w", err)
	}
	if length != pktline.Flush {
		return fmt.Errorf("expected flush-pkt after capability list, got %04x", length)
	}
	return nil
}

// Encode writes a v2 capability advertisement to a pkt-line stream.
// It writes the "version N\n" line (where N is ca.Version), then each
// capability on its own line, and terminates with a flush packet.
// Encode returns an error if ca.Version is not V2.
func (ca *CapabilityAdv) Encode(w io.Writer) error {
	if ca.Version != protocol.V2 {
		return fmt.Errorf("unsupported protocol version for capability advertisement: %s", ca.Version)
	}

	if _, err := pktline.Writef(w, "version %d\n", ca.Version); err != nil {
		return err
	}

	if err := EncodeListV2(w, &ca.Capabilities); err != nil {
		return err
	}

	return pktline.WriteFlush(w)
}

var errInvalidVersionLine = errors.New("capability advertisement must start with version line")
