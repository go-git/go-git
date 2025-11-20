package revfile

import (
	"bufio"
	"bytes"
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/utils/binary"
)

var (
	// ErrUnsupportedVersion is returned by Decode when the rev file version
	// is not supported.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrMalformedRevFile is returned by Decode when the rev file is corrupted.
	ErrMalformedRevFile = errors.New("malformed rev file")
	// ErrUnsupportedHashFunction is returned by Decode when the rev file defines an
	// unsupported hash function.
	ErrUnsupportedHashFunction = errors.New("unsupported hash function")
	// ErrEmptyReverseIndex is returned by Decode when the rev file is empty.
	ErrEmptyReverseIndex = errors.New("reverse index is empty")

	revHeader = []byte{'R', 'I', 'D', 'X'}
)

const (
	VersionSupported        = 1
	sha1Hash         uint32 = 1
	sha256Hash       uint32 = 2
)

// Decoder reads and decodes idx files from an input stream.
type Decoder struct {
	reader  *bufio.Reader
	hasher  crypto.Hash
	hash    hash.Hash
	nextFn  stateFn
	version uint32

	objCount     int64
	packChecksum plumbing.ObjectID
	out          chan<- uint32

	m sync.Mutex
}

// NewDecoder builds a reverse index decoder.
func NewDecoder(r *bufio.Reader, objCount int64, packChecksum plumbing.ObjectID) *Decoder {
	return &Decoder{
		reader:       r,
		objCount:     objCount,
		packChecksum: packChecksum,
	}
}

// stateFn defines each individual state within the state machine that
// represents a revfile.
type stateFn func(*Decoder) (stateFn, error)

// Decode reads from the reader and decode the index positions into a channel.
func (d *Decoder) Decode(out chan<- uint32) (err error) {
	d.m.Lock()
	defer d.m.Unlock()

	if d.reader == nil {
		return fmt.Errorf("%w: nil reader", ErrMalformedRevFile)
	}

	if out == nil {
		return errors.New("nil channel")
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	d.out = out

	defer close(d.out)
	for state := readMagicNumber; state != nil; {
		state, err = state(d)
		if err != nil {
			return
		}
	}
	return
}

func readMagicNumber(d *Decoder) (stateFn, error) {
	h := make([]byte, 4)
	if _, err := io.ReadFull(d.reader, h); err != nil {
		return nil, err
	}

	if !bytes.Equal(h, revHeader) {
		return nil, ErrMalformedRevFile
	}

	return readVersion, nil
}

func readVersion(d *Decoder) (stateFn, error) {
	v, err := binary.ReadUint32(d.reader)
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
	hf, err := binary.ReadUint32(d.reader)
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
	if d.objCount == 0 {
		return nil, ErrEmptyReverseIndex
	}

	var i int64
	for i = 0; i < d.objCount; i++ {
		idx, err := binary.ReadUint32(d.reader)
		if errors.Is(err, io.EOF) {
			return nil, err
		}
		if err != nil {
			return nil, err
		}

		d.out <- idx

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

	n, err := pack.ReadFrom(d.reader)
	if err != nil {
		return nil, err
	}

	if n != int64(d.hasher.Size()) {
		return nil, fmt.Errorf("%w: wrong checksum size", ErrMalformedRevFile)
	}

	if pack.Compare(d.packChecksum.Bytes()) != 0 {
		return nil, fmt.Errorf("%w: packfile hash mismatch wanted %q got %q",
			ErrMalformedRevFile, d.packChecksum.String(), pack.String())
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

	n, err := rev.ReadFrom(d.reader)
	if err != nil {
		return nil, err
	}

	if n != int64(d.hasher.Size()) {
		return nil, fmt.Errorf("%w: wrong checksum size", ErrMalformedRevFile)
	}

	rh := d.hash.Sum(nil)
	if rev.Compare(rh) != 0 {
		return nil, fmt.Errorf("%w: rev file checksum mismatch wanted %q got %q",
			ErrMalformedRevFile, hex.EncodeToString(rh), rev.String())
	}

	_, err = d.reader.Peek(1)
	if err == nil {
		return nil, fmt.Errorf("%w: expected EOF", ErrMalformedRevFile)
	}

	return nil, nil
}
