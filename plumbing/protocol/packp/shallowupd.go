package packp

import (
	"bytes"
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
)

const (
	shallowLineLen   = 48
	unshallowLineLen = 50
)

type ShallowUpdate struct {
	Shallows   []plumbing.Hash
	Unshallows []plumbing.Hash
}

func (r *ShallowUpdate) Decode(reader io.Reader) error {
	s := pktline.NewReader(reader)

	var (
		p   []byte
		err error
	)
	for {
		_, p, err = s.ReadPacket()
		if err != nil {
			break
		}

		line := bytes.TrimSpace(p)
		switch {
		case bytes.HasPrefix(line, shallow):
			err = r.decodeShallowLine(line)
		case bytes.HasPrefix(line, unshallow):
			err = r.decodeUnshallowLine(line)
		case bytes.Equal(line, pktline.Empty):
			return nil
		}

		if err != nil {
			return err
		}
	}

	if err != nil && err != io.EOF {
		return err
	}

	return nil
}

func (r *ShallowUpdate) decodeShallowLine(line []byte) error {
	hash, err := r.decodeLine(line, shallow, shallowLineLen)
	if err != nil {
		return err
	}

	r.Shallows = append(r.Shallows, hash)
	return nil
}

func (r *ShallowUpdate) decodeUnshallowLine(line []byte) error {
	hash, err := r.decodeLine(line, unshallow, unshallowLineLen)
	if err != nil {
		return err
	}

	r.Unshallows = append(r.Unshallows, hash)
	return nil
}

func (r *ShallowUpdate) decodeLine(line, prefix []byte, expLen int) (plumbing.Hash, error) {
	if len(line) != expLen {
		return plumbing.ZeroHash, fmt.Errorf("malformed %s%q", prefix, line)
	}

	raw := string(line[expLen-40 : expLen])
	return plumbing.NewHash(raw), nil
}

func (r *ShallowUpdate) Encode(w io.Writer) error {
	e := pktline.NewWriter(w)

	for _, h := range r.Shallows {
		if _, err := e.WritePacketf("%s%s\n", shallow, h.String()); err != nil {
			return err
		}
	}

	for _, h := range r.Unshallows {
		if _, err := e.WritePacketf("%s%s\n", unshallow, h.String()); err != nil {
			return err
		}
	}

	return e.WriteFlush()
}
