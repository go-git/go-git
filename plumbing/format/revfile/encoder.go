package revfile

import (
	"crypto"
	"fmt"
	"hash"
	"io"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/utils/binary"
)

// Encoder writes reverse index files to an output stream.
type Encoder struct {
	writer io.Writer
	hash   hash.Hash
	nextFn stateFnEncode

	entries      []uint32
	packChecksum plumbing.Hash
	m            sync.Mutex
}

// stateFnEncode defines each individual state within the state machine that
// represents encoding a revfile.
type stateFnEncode func(*Encoder) (stateFnEncode, error)

// NewEncoder returns a new reverse index encoder that writes to w.
func NewEncoder(w io.Writer, h hash.Hash) *Encoder {
	return &Encoder{
		writer: w,
		hash:   h,
	}
}

// Encode encodes a reverse index from a MemoryIndex to the encoder writer.
// The reverse index maps pack offsets (sorted order) to index positions.
func (e *Encoder) Encode(idx *idxfile.MemoryIndex) (err error) {
	e.m.Lock()
	defer e.m.Unlock()

	if e.writer == nil {
		return fmt.Errorf("nil writer")
	}

	if idx == nil {
		return fmt.Errorf("nil index")
	}

	if err := e.buildReverseIndex(idx); err != nil {
		return err
	}

	for state := writeHeader; state != nil; {
		state, err = state(e)
		if err != nil {
			return
		}
	}
	return
}

// buildReverseIndex creates the reverse index mapping from the MemoryIndex.
// It maps from pack offset order to index position (sorted by hash).
func (e *Encoder) buildReverseIndex(idx *idxfile.MemoryIndex) error {
	count, err := idx.Count()
	if err != nil {
		return err
	}

	offsetToPos := make(map[uint64]uint32, count)
	entries, err := idx.Entries()
	if err != nil {
		return err
	}
	defer entries.Close()

	var pos uint32
	for {
		entry, err := entries.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		offsetToPos[entry.Offset] = pos
		pos++
	}

	entriesByOffset, err := idx.EntriesByOffset()
	if err != nil {
		return err
	}
	defer entriesByOffset.Close()

	// Build the reverse index array
	e.entries = make([]uint32, 0, count)
	for {
		entry, err := entriesByOffset.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		e.entries = append(e.entries, offsetToPos[entry.Offset])
	}

	e.packChecksum = idx.PackfileChecksum
	return nil
}

func writeHeader(e *Encoder) (stateFnEncode, error) {
	_, err := e.writer.Write(revHeader)
	if err != nil {
		return nil, err
	}

	_, err = e.hash.Write(revHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to hash header: %w", err)
	}

	return writeVersion, nil
}

func writeVersion(e *Encoder) (stateFnEncode, error) {
	err := binary.WriteUint32(io.MultiWriter(e.hash, e.writer), uint32(VersionSupported))
	if err != nil {
		return nil, fmt.Errorf("failed to hash version: %w", err)
	}

	return writeHashFunction, nil
}

func writeHashFunction(e *Encoder) (stateFnEncode, error) {
	hf := sha1Hash
	if e.hash.Size() == crypto.SHA256.Size() {
		hf = sha256Hash
	}

	err := binary.WriteUint32(e.writer, uint32(hf))
	if err != nil {
		return nil, err
	}

	err = binary.Write(e.hash, hf)
	if err != nil {
		return nil, fmt.Errorf("failed to hash function %d: %w", hf, err)
	}

	return writeEntries, nil
}

func writeEntries(e *Encoder) (stateFnEncode, error) {
	for _, entry := range e.entries {
		err := binary.WriteUint32(e.writer, entry)
		if err != nil {
			return nil, err
		}

		err = binary.Write(e.hash, entry)
		if err != nil {
			return nil, fmt.Errorf("failed to hash entry: %w", err)
		}
	}

	return writePackChecksum, nil
}

func writePackChecksum(e *Encoder) (stateFnEncode, error) {
	_, err := e.writer.Write(e.packChecksum.Bytes())
	if err != nil {
		return nil, err
	}

	_, err = e.hash.Write(e.packChecksum.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to hash pack checksum: %w", err)
	}

	return writeRevChecksum, nil
}

func writeRevChecksum(e *Encoder) (stateFnEncode, error) {
	checksum := e.hash.Sum(nil)

	_, err := e.writer.Write(checksum)
	if err != nil {
		return nil, err
	}

	return nil, nil
}
