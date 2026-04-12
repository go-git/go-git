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
	d := &decoder{r: r}

	if !d.nextLine() {
		return d.err
	}

	// Check for empty repository (flush packet)
	if isFlush(d.line) {
		return ErrEmptyAdvRefs
	}

	// Must have at least a hash
	if len(d.line) < sha1HexSize {
		return d.error("line too short for hash")
	}

	hash, err := hashFrom(d.line)
	if err != nil {
		return d.error("cannot read hash: %s", err)
	}
	remain := d.line[hash.HexSize():]

	if hash.IsZero() {
		// Empty repo: skip SP "capabilities^{}" NUL
		if len(remain) < len(noHeadMark) {
			return d.error("too short zero-id ref")
		}
		if !bytes.HasPrefix(remain, noHeadMark) {
			return d.error("malformed zero-id ref")
		}
		remain = remain[len(noHeadMark):]
	} else {
		// Normal ref: SP refname NUL
		if len(remain) < 3 {
			return d.error("line too short after hash")
		}
		if remain[0] != ' ' {
			return d.error("no space after hash")
		}
		remain = remain[1:]

		chunks := bytes.SplitN(remain, null, 2)
		if len(chunks) < 2 {
			return d.error("NULL not found")
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
	for d.nextLine() {
		if len(d.line) == 0 {
			return nil // flush packet
		}

		if bytes.HasPrefix(d.line, shallow) {
			inShallows = true
			h, err := d.parseShallowData()
			if err != nil {
				return err
			}
			a.Shallows = append(a.Shallows, h)
			continue
		}

		// Once we see shallows, refs cannot follow
		if inShallows {
			return d.error("malformed shallow prefix, found ref after shallow")
		}

		// parse ref line: hash SP refname
		name, hash, err := parseRef(d.line)
		if err != nil {
			return d.error("%s", err)
		}
		a.References = append(a.References, plumbing.NewHashReference(
			plumbing.ReferenceName(name), hash,
		))
	}

	return d.err
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

type decoder struct {
	r     io.Reader
	line  []byte
	nLine int
	err   error
}

func (d *decoder) nextLine() bool {
	if d.err != nil {
		return false
	}
	d.nLine++

	_, line, err := pktline.ReadLine(d.r)
	if err != nil {
		if errors.Is(err, io.EOF) {
			if d.nLine == 1 {
				d.err = ErrEmptyInput
			} else {
				d.error("unexpected EOF")
			}
		} else {
			d.err = err
		}
		return false
	}

	d.line = bytes.TrimSuffix(line, eol)
	return true
}

func (d *decoder) error(format string, a ...any) error {
	if d.err != nil {
		return d.err
	}
	msg := fmt.Sprintf("pkt-line %d: %s", d.nLine, fmt.Sprintf(format, a...))
	d.err = NewErrUnexpectedData(msg, d.line)
	return d.err
}

func (d *decoder) parseShallowData() (plumbing.Hash, error) {
	data := bytes.TrimPrefix(d.line, shallow)

	if len(data) != sha1HexSize && len(data) != sha256HexSize {
		return plumbing.ZeroHash, d.error("malformed shallow hash: wrong length")
	}

	h, ok := plumbing.FromHex(string(data))
	if !ok {
		return plumbing.ZeroHash, d.error("invalid hash text: %s", string(data))
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
