package revfile

import (
	"fmt"
	stdhash "hash"
	"io"
	"reflect"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
)

// encoder is the internal state for encoding a rev file.
// It is not exported to prevent reuse - each Encode call creates fresh state.
type encoder struct {
	writer   io.Writer
	hash     stdhash.Hash
	hashFunc uint32

	entries      []uint32
	packChecksum plumbing.Hash
}

// stateFnEncode defines each individual state within the state machine that
// represents encoding a revfile.
type stateFnEncode func(*encoder) (stateFnEncode, error)

// Encode encodes a reverse index from a MemoryIndex to the writer.
// The reverse index maps pack offsets (sorted order) to index positions.
// It is written in the object format of idx, which sets its hash
// function id and the hash of its checksum. An unsupported object
// format returns an error wrapping [hash.ErrUnsupportedHashFunction]
// before anything is written.
// This function is safe to call concurrently with different parameters.
func Encode(w io.Writer, idx *idxfile.MemoryIndex) error {
	if w == nil {
		return fmt.Errorf("nil writer")
	}
	v := reflect.ValueOf(w)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return fmt.Errorf("nil writer")
		}
	}

	if idx == nil {
		return fmt.Errorf("nil index")
	}

	var hashFunc uint32
	format := idx.ObjectFormat()
	switch format {
	case hash.SHA1:
		hashFunc = sha1Hash
	case hash.SHA256:
		hashFunc = sha256Hash
	default:
		return fmt.Errorf("%w: object format %q", hash.ErrUnsupportedHashFunction, format)
	}
	h, err := hash.FromObjectFormat(format)
	if err != nil {
		return err
	}

	e := &encoder{
		writer:   w,
		hash:     h,
		hashFunc: hashFunc,
	}

	if err := e.buildReverseIndex(idx); err != nil {
		return err
	}

	for state := writeHeader; state != nil; {
		var err error
		state, err = state(e)
		if err != nil {
			return err
		}
	}
	return nil
}

// buildReverseIndex creates the reverse index mapping from the MemoryIndex.
// It maps from pack offset order to index position (sorted by hash).
func (e *encoder) buildReverseIndex(idx *idxfile.MemoryIndex) error {
	count, err := idx.Count()
	if err != nil {
		return err
	}

	offsetToPos := make(map[uint64]uint32, count)
	entries, err := idx.Entries()
	if err != nil {
		return err
	}
	defer func() { _ = entries.Close() }()

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
	defer func() { _ = entriesByOffset.Close() }()

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

func writeHeader(e *encoder) (stateFnEncode, error) {
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

func writeVersion(e *encoder) (stateFnEncode, error) {
	err := binary.WriteUint32(io.MultiWriter(e.hash, e.writer), uint32(VersionSupported))
	if err != nil {
		return nil, fmt.Errorf("failed to hash version: %w", err)
	}

	return writeHashFunction, nil
}

func writeHashFunction(e *encoder) (stateFnEncode, error) {
	hf := e.hashFunc

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

func writeEntries(e *encoder) (stateFnEncode, error) {
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

func writePackChecksum(e *encoder) (stateFnEncode, error) {
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

func writeRevChecksum(e *encoder) (stateFnEncode, error) {
	checksum := e.hash.Sum(nil)

	_, err := e.writer.Write(checksum)
	return nil, err
}
