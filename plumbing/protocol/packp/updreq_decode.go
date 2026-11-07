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

// read_head_info re-reads the feature list on every command line but only sets
// fixed flags, so Git's memory does not grow with the number of lines carrying
// capabilities. go-git keeps the decoded names and values, so without these
// bounds a client could make the server hold far more than it sends. Both sit
// far above anything a push declares: Git defines fewer than forty
// capabilities, and only symref, which receive-pack does not accept, takes
// more than one value.
const (
	maxCapabilities     = 256
	maxCapabilityValues = 16
)

// Decode errors.
var (
	ErrEmpty   = errors.New("empty update-request message")
	errNoFlush = errors.New("unexpected EOF before flush line")
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

	// Every packet is read the same way, mirroring read_head_info: a shallow
	// line is recognised at any position, and capabilities come from any
	// command line carrying a null byte.
	eofErr := ErrEmpty
	for {
		if err := readLine(eofErr); err != nil {
			return err
		}
		eofErr = errNoFlush

		// Stop reading once we reach the flush line
		if length == pktline.Flush {
			break
		}

		// Match receive-pack's PACKET_READ_CHOMP_NEWLINE without stripping
		// whitespace that belongs to the reference name.
		b := bytes.TrimSuffix(payload, eol)

		// Git gates the shallow branch on a line longer than the prefix, so a
		// bare "shallow" or "shallow " is parsed as a command instead.
		if len(b) > len(shallow) && bytes.HasPrefix(b, shallow) {
			h, err := parseShallow(b)
			if err != nil {
				return err
			}
			req.Shallows = append(req.Shallows, h)
			continue
		}

		cmdLine := b
		if before, after, ok := bytes.Cut(b, []byte{0}); ok {
			if len(b) < minCommandAndCapsLength {
				return errInvalidCommandCapabilitiesLineLength(len(b))
			}

			if err := decodeCapabilities(after, &req.Capabilities); err != nil {
				return err
			}
			cmdLine = before
		}

		cmd, err := parseCommand(cmdLine)
		if err != nil {
			return err
		}
		req.Commands = append(req.Commands, cmd)
	}

	// A request without commands is a no-op, as sent by a push with nothing to
	// update, and by a shallow clone that only advertises its grafts. Git's
	// read_head_info returns no commands for it, so there is nothing left to
	// validate.
	if len(req.Commands) == 0 {
		return nil
	}

	return validateUpdateRequests(req)
}

// decodeCapabilities records the capabilities a command line declares. A name
// already recorded is left alone, so a client repeating capabilities on every
// line adds nothing to the list.
func decodeCapabilities(raw []byte, dst *capability.List) error {
	var line capability.List
	capability.DecodeList(raw, &line)

	have := len(dst.All())
	for _, c := range line.All() {
		if dst.Supports(c) {
			continue
		}

		if have >= maxCapabilities {
			return errMalformedRequest(fmt.Sprintf(
				"too many capabilities: limit %d", maxCapabilities,
			))
		}

		values := line.Get(c)
		if len(values) > maxCapabilityValues {
			return errMalformedRequest(fmt.Sprintf(
				"too many values for capability %q: limit %d", c, maxCapabilityValues,
			))
		}

		dst.Add(c, values...)
		have++
	}

	return nil
}

// parseShallow parses a shallow line. Git's read_head_info tests for the
// shallow prefix on every packet, so these lines are accepted at any position
// in the request, before, between or after the command lines.
// See https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/receive-pack.c#L2204-L2211.
func parseShallow(b []byte) (plumbing.Hash, error) {
	hashLen := len(b) - len(shallow)
	if hashLen != sha1HexSize && hashLen != sha256HexSize {
		return plumbing.ZeroHash, errInvalidShallowLineLength(len(b))
	}

	h, err := parseHash(string(b[len(shallow):]))
	if err != nil {
		return plumbing.ZeroHash, errInvalidShallowObjID(err)
	}

	return h, nil
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
