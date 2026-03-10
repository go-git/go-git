package filesystem

import (
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"

	billy "github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
)

const (
	packEntryCommit   = 1
	packEntryTree     = 2
	packEntryBlob     = 3
	packEntryTag      = 4
	packEntryOFSDelta = 6
	packEntryREFDelta = 7
)

const (
	packSignature  = 0x5041434b // "PACK"
	packVersionMin = 2
	packVersionMax = 3
)

// entryMeta describes one parsed pack entry header.
type entryMeta struct {
	typ        plumbing.ObjectType
	size       int64
	dataOffset int64 // absolute

	// REF_DELTA
	baseRefHash plumbing.Hash
	// OFS_DELTA
	baseOfsOffset int64
}

// parsePackEntryType converts a pack entry type tag to a plumbing.ObjectType.
func parsePackEntryType(tag byte) (plumbing.ObjectType, error) {
	switch tag {
	case packEntryCommit:
		return plumbing.CommitObject, nil
	case packEntryTree:
		return plumbing.TreeObject, nil
	case packEntryBlob:
		return plumbing.BlobObject, nil
	case packEntryTag:
		return plumbing.TagObject, nil
	case packEntryOFSDelta:
		return plumbing.OFSDeltaObject, nil
	case packEntryREFDelta:
		return plumbing.REFDeltaObject, nil
	default:
		return plumbing.InvalidObject, fmt.Errorf("pack entry: unsupported type %d", tag)
	}
}

// readEntryMeta reads and parses one pack entry header at the given offset.
// It reads the type/size header, plus any delta base reference, and returns
// the metadata including the absolute offset of the zlib payload.
func readEntryMeta(f billy.File, offset int64, hashSize int) (entryMeta, error) {
	var meta entryMeta

	// TODO: Not necessarily true?
	maxHeaderSize := 32 + hashSize
	buf := make([]byte, maxHeaderSize)
	n, err := f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return meta, fmt.Errorf("pack entry: read header at offset %d: %w", offset, err)
	}
	if n == 0 {
		return meta, fmt.Errorf("pack entry: empty read at offset %d", offset)
	}
	buf = buf[:n]

	first := buf[0]
	tag := (first >> 4) & 0x07
	size := int64(first & 0x0f)
	pos := 1
	shift := uint(4)

	for first&0x80 != 0 {
		if pos >= len(buf) {
			return meta, fmt.Errorf("pack entry: truncated header at offset %d", offset)
		}
		first = buf[pos]
		pos++
		size |= int64(first&0x7f) << shift
		shift += 7
	}

	typ, err := parsePackEntryType(tag)
	if err != nil {
		return meta, err
	}

	meta.typ = typ
	meta.size = size
	meta.dataOffset = offset + int64(pos)

	switch typ {
	case plumbing.REFDeltaObject:
		end := pos + hashSize
		if end > len(buf) {
			return meta, fmt.Errorf("pack entry: truncated ref-delta base at offset %d", offset)
		}
		meta.baseRefHash.ResetBySize(hashSize)
		meta.baseRefHash.Write(buf[pos:end]) //nolint:errcheck
		meta.dataOffset = offset + int64(end)

	case plumbing.OFSDeltaObject:
		if pos >= len(buf) {
			return meta, fmt.Errorf("pack entry: truncated ofs-delta distance at offset %d", offset)
		}
		b := buf[pos]
		dist := int64(b & 0x7f)
		pos++
		for b&0x80 != 0 {
			if pos >= len(buf) {
				return meta, fmt.Errorf("pack entry: truncated ofs-delta distance at offset %d", offset)
			}
			b = buf[pos]
			pos++
			dist = ((dist + 1) << 7) + int64(b&0x7f)
		}
		if offset-dist < 0 {
			return meta, fmt.Errorf("pack entry: invalid ofs-delta base at offset %d", offset)
		}
		meta.baseOfsOffset = offset - dist
		meta.dataOffset = offset + int64(pos)
	}

	return meta, nil
}

// inflateFromPack inflates one zlib-compressed entry payload from the pack file
// at the given offset, returning the decompressed content as a byte slice.
// If expectedSize >= 0, the result is validated against it.
func inflateFromPack(f billy.File, offset int64, expectedSize int64) ([]byte, error) {
	sr := io.NewSectionReader(f, offset, 1<<63-1-offset)
	zr, err := zlib.NewReader(sr)
	if err != nil {
		return nil, fmt.Errorf("pack inflate: zlib open at offset %d: %w", offset, err)
	}
	defer zr.Close()

	if expectedSize >= 0 {
		body := make([]byte, expectedSize)
		if _, err := io.ReadFull(zr, body); err != nil {
			return nil, fmt.Errorf("pack inflate: read at offset %d: %w", offset, err)
		}
		return body, nil
	}

	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("pack inflate: read at offset %d: %w", offset, err)
	}
	return body, nil
}

// validatePackHeader reads and validates the signature and version.
func validatePackHeader(f billy.File) error {
	var buf [8]byte
	if _, err := f.ReadAt(buf[:], 0); err != nil {
		return fmt.Errorf("pack: cannot read header: %w", err)
	}

	sig := binary.BigEndian.Uint32(buf[0:4])
	if sig != packSignature {
		return fmt.Errorf("pack: invalid signature %#x", sig)
	}

	version := binary.BigEndian.Uint32(buf[4:8])
	if version < packVersionMin || version > packVersionMax {
		return fmt.Errorf("pack: unsupported version %d", version)
	}

	return nil
}

// readDeltaDeclaredSize reads the target size from a delta instruction stream
// header at the given pack offset. This inflates only enough data to read the
// two varint sizes at the start of the delta.
func readDeltaDeclaredSize(f billy.File, dataOffset int64) (int64, error) {
	sr := io.NewSectionReader(f, dataOffset, 1<<63-1-dataOffset)
	zr, err := zlib.NewReader(sr)
	if err != nil {
		return 0, fmt.Errorf("pack delta size: zlib open: %w", err)
	}
	defer zr.Close()

	// Read source size varint.
	if _, err := readDeltaVarint(zr); err != nil {
		return 0, fmt.Errorf("pack delta size: source varint: %w", err)
	}

	// Read target size varint.
	targetSize, err := readDeltaVarint(zr)
	if err != nil {
		return 0, fmt.Errorf("pack delta size: target varint: %w", err)
	}

	return targetSize, nil
}

// readDeltaVarint reads one Git delta varint from a reader.
func readDeltaVarint(r io.Reader) (int64, error) {
	var value int64
	var shift uint
	var buf [1]byte

	for {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		b := buf[0]
		value |= int64(b&0x7f) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
		if shift > 63 {
			return 0, fmt.Errorf("delta varint overflow")
		}
	}

	return value, nil
}
