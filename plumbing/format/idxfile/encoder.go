package idxfile

import (
	"fmt"
	"hash"
	"io"

	"github.com/go-git/go-git/v6/utils/binary"
)

// encoder is the internal state for encoding an idx file.
// It is not exported to prevent reuse - each Encode call creates fresh state.
type encoder struct {
	writer  io.Writer
	hashSum func() []byte
	idx     *MemoryIndex
}

// stateFnEncode defines each individual state within the state machine that
// represents encoding an idxfile.
type stateFnEncode func(*encoder) (stateFnEncode, error)

// Encode encodes a MemoryIndex to the writer.
// This function is safe to call concurrently with different parameters.
func Encode(w io.Writer, h hash.Hash, idx *MemoryIndex) error {
	if w == nil {
		return fmt.Errorf("nil writer")
	}

	if idx == nil {
		return fmt.Errorf("nil index")
	}

	e := &encoder{
		writer:  io.MultiWriter(w, h),
		hashSum: func() []byte { return h.Sum(nil) },
		idx:     idx,
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

func writeHeader(e *encoder) (stateFnEncode, error) {
	if e.idx.Version != VersionSupported {
		return nil, ErrUnsupportedVersion
	}

	_, err := e.writer.Write(idxHeader)
	if err != nil {
		return nil, err
	}

	err = binary.WriteUint32(e.writer, e.idx.Version)
	if err != nil {
		return nil, err
	}

	return writeFanout, nil
}

func writeFanout(e *encoder) (stateFnEncode, error) {
	for _, c := range e.idx.Fanout {
		if err := binary.WriteUint32(e.writer, c); err != nil {
			return nil, err
		}
	}

	return writeHashes, nil
}

func writeHashes(e *encoder) (stateFnEncode, error) {
	for k := range fanout {
		pos := e.idx.FanoutMapping[k]
		if pos == noMapping {
			continue
		}

		if pos >= len(e.idx.Names) {
			return nil, fmt.Errorf("%w: invalid position %d", ErrMalformedIdxFile, pos)
		}

		_, err := e.writer.Write(e.idx.Names[pos])
		if err != nil {
			return nil, err
		}
	}

	return writeCRC32, nil
}

func writeCRC32(e *encoder) (stateFnEncode, error) {
	for k := range fanout {
		pos := e.idx.FanoutMapping[k]
		if pos == noMapping {
			continue
		}

		if pos >= len(e.idx.CRC32) {
			return nil, fmt.Errorf("%w: invalid CRC32 index %d", ErrMalformedIdxFile, pos)
		}

		_, err := e.writer.Write(e.idx.CRC32[pos])
		if err != nil {
			return nil, err
		}
	}

	return writeOffsets, nil
}

func writeOffsets(e *encoder) (stateFnEncode, error) {
	for k := range fanout {
		pos := e.idx.FanoutMapping[k]
		if pos == noMapping {
			continue
		}

		if pos >= len(e.idx.Offset32) {
			return nil, fmt.Errorf("%w: invalid offset32 index %d", ErrMalformedIdxFile, pos)
		}

		_, err := e.writer.Write(e.idx.Offset32[pos])
		if err != nil {
			return nil, err
		}
	}

	if len(e.idx.Offset64) > 0 {
		_, err := e.writer.Write(e.idx.Offset64)
		if err != nil {
			return nil, err
		}
	}

	return writeChecksums, nil
}

func writeChecksums(e *encoder) (stateFnEncode, error) {
	_, err := e.writer.Write(e.idx.PackfileChecksum.Bytes())
	if err != nil {
		return nil, err
	}

	checksum := e.hashSum()
	if _, err := e.idx.IdxChecksum.Write(checksum); err != nil {
		return nil, err
	}

	_, err = e.writer.Write(e.idx.IdxChecksum.Bytes())
	if err != nil {
		return nil, err
	}

	return nil, nil
}
