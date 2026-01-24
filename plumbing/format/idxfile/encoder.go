package idxfile

import (
	"crypto"
	"io"

	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
)

// Encoder writes MemoryIndex structs to an output stream.
type Encoder struct {
	io.Writer
	hash hash.Hash
}

// NewEncoder returns a new stream encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	// TODO: Support passing an ObjectFormat (sha256)
	h := hash.New(crypto.SHA1)
	mw := io.MultiWriter(w, h)
	return &Encoder{mw, h}
}

// Encode encodes an MemoryIndex to the encoder writer.
func (e *Encoder) Encode(idx *MemoryIndex) (int, error) {
	flow := []func(*MemoryIndex) (int, error){
		e.encodeHeader,
		e.encodeFanout,
		e.encodeHashes,
		e.encodeCRC32,
		e.encodeOffsets,
		e.encodeChecksums,
	}

	sz := 0
	for _, f := range flow {
		i, err := f(idx)
		sz += i

		if err != nil {
			return sz, err
		}
	}

	return sz, nil
}

func (e *Encoder) encodeHeader(idx *MemoryIndex) (int, error) {
	c, err := e.Write(idxHeader)
	if err != nil {
		return c, err
	}

	return c + 4, binary.WriteUint32(e, idx.Version)
}

func (e *Encoder) encodeFanout(idx *MemoryIndex) (int, error) {
	for _, c := range idx.Fanout {
		if err := binary.WriteUint32(e, c); err != nil {
			return 0, err
		}
	}

	return fanout * 4, nil
}

func (e *Encoder) encodeHashes(idx *MemoryIndex) (int, error) {
	var size int
	for k := range fanout {
		pos := idx.FanoutMapping[k]
		if pos == noMapping {
			continue
		}

		n, err := e.Write(idx.Names[pos])
		if err != nil {
			return size, err
		}
		size += n
	}
	return size, nil
}

func (e *Encoder) encodeCRC32(idx *MemoryIndex) (int, error) {
	var size int
	for k := range fanout {
		pos := idx.FanoutMapping[k]
		if pos == noMapping {
			continue
		}

		n, err := e.Write(idx.CRC32[pos])
		if err != nil {
			return size, err
		}

		size += n
	}

	return size, nil
}

func (e *Encoder) encodeOffsets(idx *MemoryIndex) (int, error) {
	var size int
	for k := range fanout {
		pos := idx.FanoutMapping[k]
		if pos == noMapping {
			continue
		}

		n, err := e.Write(idx.Offset32[pos])
		if err != nil {
			return size, err
		}

		size += n
	}

	if len(idx.Offset64) > 0 {
		n, err := e.Write(idx.Offset64)
		if err != nil {
			return size, err
		}

		size += n
	}

	return size, nil
}

func (e *Encoder) encodeChecksums(idx *MemoryIndex) (int, error) {
	n1, err := e.Write(idx.PackfileChecksum.Bytes())
	if err != nil {
		return 0, err
	}

	if _, err := idx.IdxChecksum.Write(e.hash.Sum(nil)[:e.hash.Size()]); err != nil {
		return 0, err
	}

	n2, err := e.Write(idx.IdxChecksum.Bytes())
	if err != nil {
		return 0, err
	}

	return n1 + n2, nil
}
