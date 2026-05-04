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
	// ErrEmptyAdvRefs is returned by Decode if it gets an empty advertised
	// references message.
	ErrEmptyAdvRefs = errors.New("empty advertised-ref message")
	// ErrEmptyInput is returned by Decode if the input is empty.
	ErrEmptyInput = errors.New("empty input")
)

// Decode reads the next advertised-refs message form its input and
// stores it in the AdvRefs.
func (a *AdvRefs) Decode(r io.Reader) error {
	var (
		nLine int
		line  []byte
		err   error
	)

	nextLine := func() bool {
		nLine++
		_, p, e := pktline.ReadLine(r)
		if e != nil {
			if errors.Is(e, io.EOF) {
				if nLine == 1 {
					err = ErrEmptyInput
				} else {
					err = NewErrUnexpectedData(fmt.Sprintf("pkt-line %d: unexpected EOF", nLine), line)
				}
			} else {
				err = e
			}
			return false
		}
		line = bytes.TrimSuffix(p, eol)
		return true
	}

	decodeError := func(format string, a ...any) error {
		msg := fmt.Sprintf("pkt-line %d: %s", nLine, fmt.Sprintf(format, a...))
		return NewErrUnexpectedData(msg, line)
	}

	if !nextLine() {
		return err
	}

	// Check for empty repository (flush packet)
	if isFlush(line) {
		return ErrEmptyAdvRefs
	}

	// Must have at least a hash
	if len(line) < sha1HexSize {
		return decodeError("line too short for hash")
	}

	hash, e := hashFrom(line)
	if e != nil {
		return decodeError("cannot read hash: %s", e)
	}
	remain := line[hash.HexSize():]

	if hash.IsZero() {
		// Empty repo: skip SP "capabilities^{}" NUL
		if len(remain) < len(noHeadMark) {
			return decodeError("too short zero-id ref")
		}
		if !bytes.HasPrefix(remain, noHeadMark) {
			return decodeError("malformed zero-id ref")
		}
		remain = remain[len(noHeadMark):]
	} else {
		// Normal ref: SP refname NUL
		if len(remain) < 3 {
			return decodeError("line too short after hash")
		}
		if remain[0] != ' ' {
			return decodeError("no space after hash")
		}
		remain = remain[1:]

		chunks := bytes.SplitN(remain, null, 2)
		if len(chunks) < 2 {
			return decodeError("NULL not found")
		}
		a.References = append(a.References, plumbing.NewHashReference(
			plumbing.ReferenceName(chunks[0]), hash,
		))
		remain = chunks[1]
	}

	// Decode capabilities
	capability.DecodeList(remain, &a.Capabilities)

	// Decode remaining refs and shallows
	inShallows := false
	for nextLine() {
		if len(line) == 0 {
			return nil // flush packet
		}

		if bytes.HasPrefix(line, shallow) {
			inShallows = true
			data := bytes.TrimPrefix(line, shallow)

			if len(data) != sha1HexSize && len(data) != sha256HexSize {
				return decodeError("malformed shallow hash: wrong length")
			}

			h, ok := plumbing.FromHex(string(data))
			if !ok {
				return decodeError("invalid hash text: %s", string(data))
			}
			a.Shallows = append(a.Shallows, h)
			continue
		}

		// Once we see shallows, refs cannot follow
		if inShallows {
			return decodeError("malformed shallow prefix, found ref after shallow")
		}

		// parse ref line: hash SP refname
		name, hash, e := parseRef(line)
		if e != nil {
			return decodeError("%s", e)
		}
		a.References = append(a.References, plumbing.NewHashReference(
			plumbing.ReferenceName(name), hash,
		))
	}

	return err
}

func hashFrom(line []byte) (plumbing.Hash, error) {
	hashSize := bytes.IndexByte(line, ' ')
	if hashSize == -1 {
		hashSize = len(line)
	}
	if hashSize != sha1HexSize && hashSize != sha256HexSize {
		return plumbing.ZeroHash, fmt.Errorf("cannot read hash, invalid size: %d", hashSize)
	}

	h, ok := plumbing.FromHex(string(line[:hashSize]))
	if !ok {
		return plumbing.ZeroHash, fmt.Errorf("invalid hash text: %s", line[:hashSize])
	}

	return h, nil
}

func parseRef(data []byte) (string, plumbing.Hash, error) {
	before, after, ok := bytes.Cut(data, []byte{' '})
	if !ok {
		return "", plumbing.ZeroHash, fmt.Errorf("malformed ref data: no space")
	}
	if bytes.IndexByte(after, ' ') != -1 {
		return "", plumbing.ZeroHash, fmt.Errorf("malformed ref data: multiple spaces")
	}
	return string(after), plumbing.NewHash(string(before)), nil
}
