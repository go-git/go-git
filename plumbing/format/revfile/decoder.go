package revfile

import (
	"bufio"
	"bytes"
	"crypto"
	"hash"
	"sync"

	"errors"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/go-git/go-git/v6/utils/binary"
)

var (
	// ErrUnsupportedVersion is returned by Decode when the rev file version
	// is not supported.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrMalformedRevFile is returned by Decode when the rev file is corrupted.
	ErrMalformedRevFile = errors.New("malformed rev file")
	// ErrNilIndex is returned by Decode when a nil index is used.
	ErrNilIndex = errors.New("nil index")
	// ErrUnsupportedHashFunction is returned by Decode when the rev file defines an
	// unsupported hash function.
	ErrUnsupportedHashFunction = errors.New("unsupported hash function")
	// ErrEmptyReverseIndex is returned by Decode when the rev file is empty.
	ErrEmptyReverseIndex = errors.New("reverse index is empty")

	revHeader = []byte{'R', 'I', 'D', 'X'}
)

const (
	VersionSupported = 1
	sha1Hash         = 1
	sha256Hash       = 2
)

// Decoder reads and decodes idx files from an input stream.
type Decoder struct {
	*bufio.Reader
	hasher  crypto.Hash
	hash    hash.Hash
	nextFn  stateFn
	version uint32

	idx *idxfile.MemoryIndex
	m   sync.Mutex
}

// NewDecoder builds a new idx stream decoder, that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{Reader: bufio.NewReader(r)}
}

// stateFn defines each individual state within the state machine that
// represents a revfile.
type stateFn func(*Decoder) (stateFn, error)

// Decode reads from the stream and decode the content into the MemoryIndex struct.
func (d *Decoder) Decode(idx *idxfile.MemoryIndex) error {
	if idx == nil {
		return ErrNilIndex
	}

	d.m.Lock()
	defer d.m.Unlock()

	d.idx = idx

	var err error
	for state := readMagicNumber; state != nil; {
		state, err = state(d)
		if err != nil {
			return err
		}
	}
	return nil
}

func readMagicNumber(d *Decoder) (stateFn, error) {
	var h = make([]byte, 4)
	if _, err := io.ReadFull(d.Reader, h); err != nil {
		return nil, err
	}

	if !bytes.Equal(h, revHeader) {
		return nil, ErrMalformedRevFile
	}

	return readVersion, nil
}

func readVersion(d *Decoder) (stateFn, error) {
	v, err := binary.ReadUint32(d.Reader)
	if err != nil {
		return nil, err
	}

	if v != VersionSupported {
		return nil, ErrUnsupportedVersion
	}
	d.version = v

	return readHashFunction, nil
}

func readHashFunction(d *Decoder) (stateFn, error) {
	hf, err := binary.ReadUint32(d.Reader)
	if err != nil {
		return nil, err
	}

	switch hf {
	case sha1Hash:
		d.hasher = crypto.SHA1
	case sha256Hash:
		d.hasher = crypto.SHA256
	default:
		return nil, ErrUnsupportedHashFunction
	}

	d.hash = d.hasher.New()
	err = binary.Write(d.hash, revHeader, d.version, hf)
	if err != nil {
		return nil, fmt.Errorf("failed to hash rev header: %w", err)
	}

	return readEntries, nil
}

func readEntries(d *Decoder) (stateFn, error) {
	count, err := d.idx.Count()

	if err != nil {
		return nil, fmt.Errorf("failed to get idx count: %w", err)
	}

	if count == 0 {
		return nil, ErrEmptyReverseIndex
	}

	var i int64
	for i = 0; i < count; i++ {
		idx, err := binary.ReadUint32(d.Reader)
		if errors.Is(err, io.EOF) {
			return nil, err
		}
		if err != nil {
			return nil, err
		}

		err = binary.Write(d.hash, idx)
		if err != nil {
			return nil, fmt.Errorf("failed to hash entry: %w", err)
		}
	}

	return readPackChecksum, nil
}

func readPackChecksum(d *Decoder) (stateFn, error) {
	var pack plumbing.Hash
	pack.ResetBySize(d.hasher.Size())

	n, err := pack.ReadFrom(d.Reader)
	if err != nil {
		return nil, err
	}

	if n != int64(d.hasher.Size()) {
		return nil, fmt.Errorf("%w: wrong checksum size", ErrMalformedRevFile)
	}

	if pack.Compare(d.idx.PackfileChecksum.Bytes()) != 0 {
		return nil, fmt.Errorf("%w: pack file hash mismatch", ErrMalformedRevFile)
	}

	err = binary.Write(d.hash, pack.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to hash pack checksum: %w", err)
	}

	return readRevChecksum, nil
}

func readRevChecksum(d *Decoder) (stateFn, error) {
	var rev plumbing.Hash
	rev.ResetBySize(d.hasher.Size())
	n, err := rev.ReadFrom(d.Reader)
	if err != nil {
		return nil, err
	}

	if n != int64(d.hasher.Size()) {
		return nil, fmt.Errorf("%w: wrong checksum size", ErrMalformedRevFile)
	}

	if rev.Compare(d.hash.Sum(nil)) != 0 {
		return nil, fmt.Errorf("%w: rev file checksum mismatch", ErrMalformedRevFile)
	}

	return nil, nil
}
