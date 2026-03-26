package idxfile

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
)

var (
	// ErrUnsupportedVersion is returned by Decode when the idx file version
	// is not supported.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrMalformedIdxFile is returned by Decode when the idx file is corrupted.
	ErrMalformedIdxFile = errors.New("malformed idx file")
)

const (
	fanout = 256
)

// Decoder reads and decodes idx files from an input stream.
type Decoder struct {
	io.Reader
	h hash.Hash
}

// NewDecoder builds a new idx stream decoder, that reads from r.
func NewDecoder(r io.Reader, h hash.Hash) *Decoder {
	tr := io.TeeReader(r, h)
	return &Decoder{tr, h}
}

// Decode reads from the stream and decode the content into the MemoryIndex struct.
func (d *Decoder) Decode(idx *MemoryIndex) error {
	d.h.Reset()
	if err := validateHeader(d); err != nil {
		return err
	}

	flow := []func(*MemoryIndex, io.Reader) error{
		readVersion,
		readFanout,
		readObjectNames,
		readCRC32,
		readOffsets,
		readPackChecksum,
	}

	for _, f := range flow {
		if err := f(idx, d); err != nil {
			return err
		}
	}

	actual := d.h.Sum(nil)
	if err := readIdxChecksum(idx, d); err != nil {
		return err
	}

	if idx.IdxChecksum.Compare(actual) != 0 {
		return fmt.Errorf("%w: checksum mismatch: %q instead of %q",
			ErrMalformedIdxFile, idx.IdxChecksum.String(), hex.EncodeToString(actual))
	}

	return nil
}

func validateHeader(r io.Reader) error {
	h := make([]byte, 4)
	if _, err := io.ReadFull(r, h); err != nil {
		return err
	}

	if !bytes.Equal(h, idxHeader) {
		return ErrMalformedIdxFile
	}

	return nil
}

func readVersion(idx *MemoryIndex, r io.Reader) error {
	v, err := binary.ReadUint32(r)
	if err != nil {
		return err
	}

	if v > VersionSupported {
		return ErrUnsupportedVersion
	}

	idx.Version = v
	return nil
}

func readFanout(idx *MemoryIndex, r io.Reader) error {
	for k := range fanout {
		n, err := binary.ReadUint32(r)
		if err != nil {
			return err
		}

		idx.Fanout[k] = n
		idx.FanoutMapping[k] = noMapping
	}

	return nil
}

func readObjectNames(idx *MemoryIndex, r io.Reader) error {
	for k := range fanout {
		var buckets uint32
		if k == 0 {
			buckets = idx.Fanout[k]
		} else {
			buckets = idx.Fanout[k] - idx.Fanout[k-1]
		}

		if buckets == 0 {
			continue
		}

		idx.FanoutMapping[k] = len(idx.Names)

		nameLen := int(buckets * uint32(idx.idSize()))
		bin := make([]byte, nameLen)
		if _, err := io.ReadFull(r, bin); err != nil {
			return err
		}

		idx.Names = append(idx.Names, bin)
		idx.Offset32 = append(idx.Offset32, make([]byte, buckets*4))
		idx.CRC32 = append(idx.CRC32, make([]byte, buckets*4))
	}

	return nil
}

func readCRC32(idx *MemoryIndex, r io.Reader) error {
	for k := range fanout {
		if pos := idx.FanoutMapping[k]; pos != noMapping {
			if _, err := io.ReadFull(r, idx.CRC32[pos]); err != nil {
				return err
			}
		}
	}

	return nil
}

func readOffsets(idx *MemoryIndex, r io.Reader) error {
	var o64cnt int
	for k := range fanout {
		if pos := idx.FanoutMapping[k]; pos != noMapping {
			if _, err := io.ReadFull(r, idx.Offset32[pos]); err != nil {
				return err
			}

			for p := 0; p < len(idx.Offset32[pos]); p += 4 {
				if idx.Offset32[pos][p]&(byte(1)<<7) > 0 {
					o64cnt++
				}
			}
		}
	}

	if o64cnt > 0 {
		idx.Offset64 = make([]byte, o64cnt*8)
		if _, err := io.ReadFull(r, idx.Offset64); err != nil {
			return err
		}
	}

	return nil
}

func readPackChecksum(idx *MemoryIndex, r io.Reader) error {
	idx.PackfileChecksum.ResetBySize(idx.idSize())
	if _, err := idx.PackfileChecksum.ReadFrom(r); err != nil {
		return err
	}

	return nil
}

func readIdxChecksum(idx *MemoryIndex, r io.Reader) error {
	idx.IdxChecksum.ResetBySize(idx.idSize())
	if _, err := idx.IdxChecksum.ReadFrom(r); err != nil {
		return err
	}

	return nil
}

// MaxIdxFileSize is the maximum size of an .idx file that DecodeLazy will
// accept. This guards against malicious or corrupted streams that could
// cause excessive memory allocation. 4 GiB covers the largest realistic
// v2 idx files (which are bounded by 2^32 objects × ~28 bytes each).
//
// DecodeLazy enforces this limit internally. External call sites that read
// .idx bytes with io.ReadAll before calling DecodeLazy (e.g. dumb HTTP
// transport, NewPackfileIter) SHOULD also wrap the reader with
// io.LimitReader(r, MaxIdxFileSize+1) and reject oversized streams early,
// before allocating additional buffers for .rev generation.
const MaxIdxFileSize = 4 << 30 // 4 GiB

// DecodeLazy reads the entire .idx stream from r into memory, validates the
// trailing idx checksum, and returns a [*LazyIndex] backed by the in-memory
// buffer. The returned LazyIndex retains the buffer for its entire lifetime;
// callers must not reuse or modify the reader after calling DecodeLazy.
// The caller is responsible for supplying revOpener; DecodeLazy does
// not generate a .rev file.
//
// h is used to hash the idx bytes for checksum validation and must match the
// hash function used when writing the idx (SHA-1 or SHA-256).
//
// packHash is the expected packfile checksum embedded in the idx; it is
// forwarded to [NewLazyIndex] which validates it during initialisation.
func DecodeLazy(r io.Reader, h hash.Hash, revOpener func() (ReadAtCloser, error), packHash plumbing.Hash) (*LazyIndex, error) {
	if revOpener == nil {
		return nil, errors.New("idxfile: DecodeLazy: revOpener must not be nil")
	}

	buf, err := io.ReadAll(io.LimitReader(r, MaxIdxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("idxfile: DecodeLazy: read stream: %w", err)
	}
	if len(buf) > MaxIdxFileSize {
		return nil, fmt.Errorf("%w: DecodeLazy: idx stream exceeds %d byte limit", ErrMalformedIdxFile, MaxIdxFileSize)
	}

	hashSize := packHash.Size()
	if len(buf) < 2*hashSize {
		return nil, fmt.Errorf("%w: DecodeLazy: buffer too short (%d bytes, need at least %d)", ErrMalformedIdxFile, len(buf), 2*hashSize)
	}

	// The trailing hashSize bytes are the idx checksum.
	// Everything before it is the content that was hashed.
	body := buf[:len(buf)-hashSize]
	storedChecksum := buf[len(buf)-hashSize:]

	h.Reset()
	_, _ = h.Write(body) // hash.Hash.Write never returns an error
	computed := h.Sum(nil)
	if !bytes.Equal(computed, storedChecksum) {
		return nil, fmt.Errorf("%w: DecodeLazy: checksum mismatch: got %x, want %x",
			ErrMalformedIdxFile, computed, storedChecksum)
	}

	openIdx := func() (ReadAtCloser, error) {
		return nopCloserReaderAt{bytes.NewReader(buf)}, nil
	}

	return NewLazyIndex(openIdx, revOpener, packHash)
}
