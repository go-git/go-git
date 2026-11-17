package packp

import (
	"bytes"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

// ShallowUpdate represents shallow/unshallow updates during fetch.
type ShallowUpdate struct {
	Shallows   []plumbing.Hash
	Unshallows []plumbing.Hash
}

// Decode parses shallow update information from the reader.
func (r *ShallowUpdate) Decode(reader io.Reader) error {
	s := pktline.NewScanner(reader)
	for s.Scan() {
		if s.Len() == pktline.Flush {
			return nil
		}

		line := bytes.TrimSpace(s.Bytes())
		var err error
		switch {
		case bytes.HasPrefix(line, shallow):
			err = r.decodeShallowLine(line)
		case bytes.HasPrefix(line, unshallow):
			err = r.decodeUnshallowLine(line)
		default:
			err = fmt.Errorf("unexpected shallow line: %q", line)
		}

		if err != nil {
			return err
		}
	}

	return s.Err()
}

func (r *ShallowUpdate) decodeShallowLine(line []byte) error {
	hash, err := r.decodeLine(line, shallow)
	if err != nil {
		return err
	}

	r.Shallows = append(r.Shallows, hash)
	return nil
}

func (r *ShallowUpdate) decodeUnshallowLine(line []byte) error {
	hash, err := r.decodeLine(line, unshallow)
	if err != nil {
		return err
	}

	r.Unshallows = append(r.Unshallows, hash)
	return nil
}

func (r *ShallowUpdate) decodeLine(line, prefix []byte) (plumbing.Hash, error) {
	hash, ok := parseFullHash(string(line[len(prefix):]))
	if !ok {
		return plumbing.ZeroHash, fmt.Errorf("malformed shallow update line: %q", line)
	}
	return hash, nil
}

// Encode writes the shallow update to the writer.
func (r *ShallowUpdate) Encode(w io.Writer) error {
	for _, h := range r.Shallows {
		if _, err := pktline.Writef(w, "%s%s", shallow, h.String()); err != nil {
			return err
		}
	}

	for _, h := range r.Unshallows {
		if _, err := pktline.Writef(w, "%s%s", unshallow, h.String()); err != nil {
			return err
		}
	}

	return pktline.WriteFlush(w)
}
