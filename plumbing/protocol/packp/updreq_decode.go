package packp

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

var (
	minCommandLength        = sha1HexSize*2 + 2 + 1
	minCommandAndCapsLength = minCommandLength + 1
)

// Decode errors.
var (
	ErrEmpty                        = errors.New("empty update-request message")
	errNoCommands                   = errors.New("unexpected EOF before any command")
	errMissingCapabilitiesDelimiter = errors.New("capabilities delimiter not found")
	errNoFlush                      = errors.New("unexpected EOF before flush line")
)

func errMalformedRequest(reason string) error {
	return fmt.Errorf("malformed request: %s", reason)
}

func errInvalidHash(hash string) error {
	return fmt.Errorf("invalid hash: %s", hash)
}

func errInvalidShallowLineLength(got int) error {
	return errMalformedRequest(fmt.Sprintf(
		"invalid shallow line length: expected %d or %d, got %d",
		len(shallow)+sha1HexSize, len(shallow)+sha256HexSize, got,
	))
}

func errInvalidCommandCapabilitiesLineLength(got int) error {
	return errMalformedRequest(fmt.Sprintf(
		"invalid command and capabilities line length: expected at least %d, got %d",
		minCommandAndCapsLength, got,
	))
}

func errInvalidCommandLineLength(got int) error {
	return errMalformedRequest(fmt.Sprintf(
		"invalid command line length: expected at least %d, got %d",
		minCommandLength, got,
	))
}

func errInvalidShallowObjID(err error) error {
	return errMalformedRequest(
		fmt.Sprintf("invalid shallow object id: %s", err.Error()),
	)
}

func errInvalidOldObjID(err error) error {
	return errMalformedRequest(
		fmt.Sprintf("invalid old object id: %s", err.Error()),
	)
}

func errInvalidNewObjID(err error) error {
	return errMalformedRequest(
		fmt.Sprintf("invalid new object id: %s", err.Error()),
	)
}

func errMalformedCommand(err error) error {
	return errMalformedRequest(fmt.Sprintf(
		"malformed command: %s", err.Error(),
	))
}

// Decode reads the next update-request message from the reader.
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/receive-pack.c#L2562-L2566
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/pkt-line.c#L466-L493
func (req *UpdateRequests) Decode(r io.Reader) error {
	var (
		payload []byte
		length  int
	)

	s := pktline.NewScanner(r)

	readLine := func(eofErr error) error {
		if !s.Scan() {
			if s.Err() == nil {
				return eofErr
			}
			return s.Err()
		}
		length = s.Len()
		if length == pktline.Flush {
			payload = nil
		} else {
			payload = s.Bytes()
		}
		return nil
	}

	// Scan first line
	if err := readLine(ErrEmpty); err != nil {
		return err
	}

	// Process all consecutive shallow lines
	for {
		b := bytes.TrimSuffix(payload, eol)
		if !bytes.HasPrefix(b, shallowNoSp) {
			break
		}

		hashLen := len(b) - len(shallow)
		if hashLen != sha1HexSize && hashLen != sha256HexSize {
			return errInvalidShallowLineLength(len(b))
		}

		h, err := parseHash(string(b[len(shallow):]))
		if err != nil {
			return errInvalidShallowObjID(err)
		}
		req.Shallows = append(req.Shallows, h)

		if err := readLine(errNoCommands); err != nil {
			return err
		}
	}

	// A shallow-only no-op push (shallow lines followed immediately by a
	// flush, with no commands) is a valid empty request, e.g. from a shallow
	// clone with nothing to push. A bare flush with no shallows is still
	// treated as malformed.
	if length == pktline.Flush && len(req.Shallows) > 0 {
		return nil
	}

	// The first command line must contain capabilities separated by a null byte
	before, after, ok := bytes.Cut(payload, []byte{0})
	if !ok {
		return errMissingCapabilitiesDelimiter
	}
	if len(payload) < minCommandAndCapsLength {
		return errInvalidCommandCapabilitiesLineLength(len(payload))
	}

	// Extract and decode capabilities (everything after the null byte)
	capability.DecodeList(after, &req.Capabilities)

	// Extract the command (everything before the null byte)
	cmd, err := parseCommand(before)
	if err != nil {
		return err
	}
	req.Commands = append(req.Commands, cmd)

	// Read and process remaining commands
	for {
		if err := readLine(errNoFlush); err != nil {
			return err
		}

		// Stop reading once we reach the flush line
		if length == pktline.Flush {
			break
		}

		// Match receive-pack's PACKET_READ_CHOMP_NEWLINE without stripping
		// whitespace that belongs to the reference name.
		cmd, err := parseCommand(bytes.TrimSuffix(payload, eol))
		if err != nil {
			return err
		}
		req.Commands = append(req.Commands, cmd)
	}

	// We should always have a flush line at the end of the request.
	if len(payload) != 0 || length != pktline.Flush {
		return errMalformedRequest("unexpected data after flush")
	}

	return validateUpdateRequests(req)
}

// parseCommand preserves the complete reference name after the two object IDs.
// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/receive-pack.c#L2144-L2152.
func parseCommand(b []byte) (*Command, error) {
	if len(b) < minCommandLength {
		return nil, errInvalidCommandLineLength(len(b))
	}

	oldHex, rest, ok := bytes.Cut(b, []byte{' '})
	if !ok {
		return nil, errMalformedCommand(io.EOF)
	}
	newHex, name, ok := bytes.Cut(rest, []byte{' '})
	if !ok || len(name) == 0 {
		return nil, errMalformedCommand(io.EOF)
	}

	oh, err := parseHash(string(oldHex))
	if err != nil {
		return nil, errInvalidOldObjID(err)
	}

	nh, err := parseHash(string(newHex))
	if err != nil {
		return nil, errInvalidNewObjID(err)
	}

	// Git's queue_command (builtin/receive-pack.c) keeps the entire remainder
	// after the two object IDs. The receive-pack name gate must see whitespace
	// in that remainder rather than update a different, truncated reference.
	return &Command{Old: oh, New: nh, Name: plumbing.ReferenceName(name)}, nil
}

func parseHash(s string) (plumbing.Hash, error) {
	if len(s) != sha1HexSize && len(s) != sha256HexSize {
		return plumbing.ZeroHash, errInvalidHash(s)
	}
	h, ok := plumbing.FromHex(s)
	if !ok {
		return plumbing.ZeroHash, errInvalidHash(s)
	}

	return h, nil
}
