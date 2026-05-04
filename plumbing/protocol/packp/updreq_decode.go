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
		len(shallow)+sha1HexSize, len(shallow)+sha256HexSize, got))
}

func errInvalidCommandCapabilitiesLineLength(got int) error {
	return errMalformedRequest(fmt.Sprintf(
		"invalid command and capabilities line length: expected at least %d, got %d",
		minCommandAndCapsLength, got))
}

func errInvalidCommandLineLength(got int) error {
	return errMalformedRequest(fmt.Sprintf(
		"invalid command line length: expected at least %d, got %d",
		minCommandLength, got))
}

func errInvalidShallowObjID(err error) error {
	return errMalformedRequest(
		fmt.Sprintf("invalid shallow object id: %s", err.Error()))
}

func errInvalidOldObjID(err error) error {
	return errMalformedRequest(
		fmt.Sprintf("invalid old object id: %s", err.Error()))
}

func errInvalidNewObjID(err error) error {
	return errMalformedRequest(
		fmt.Sprintf("invalid new object id: %s", err.Error()))
}

func errMalformedCommand(err error) error {
	return errMalformedRequest(fmt.Sprintf(
		"malformed command: %s", err.Error()))
}

// Decode reads the next update-request message from the reader.
func (req *UpdateRequests) Decode(r io.Reader) error {
	var (
		payload []byte
		length  int
	)

	readLine := func(eofErr error) error {
		l, p, err := pktline.ReadLine(r)
		if errors.Is(err, io.EOF) {
			return eofErr
		}
		if err != nil {
			return err
		}
		payload = p
		length = l
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

		cmd, err := parseCommand(payload)
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

func parseCommand(b []byte) (*Command, error) {
	if len(b) < minCommandLength {
		return nil, errInvalidCommandLineLength(len(b))
	}

	var (
		os, ns string
		n      plumbing.ReferenceName
	)
	if _, err := fmt.Sscanf(string(b), "%s %s %s", &os, &ns, &n); err != nil {
		return nil, errMalformedCommand(err)
	}

	oh, err := parseHash(os)
	if err != nil {
		return nil, errInvalidOldObjID(err)
	}

	nh, err := parseHash(ns)
	if err != nil {
		return nil, errInvalidNewObjID(err)
	}

	return &Command{Old: oh, New: nh, Name: n}, nil
}

func parseHash(s string) (plumbing.Hash, error) {
	h, ok := plumbing.FromHex(s)
	if !ok {
		return plumbing.ZeroHash, errInvalidHash(s)
	}

	return h, nil
}
