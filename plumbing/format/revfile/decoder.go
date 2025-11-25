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

// decoder is the internal state for decoding a rev file.
// It is not exported to prevent reuse - each Decode call creates fresh state.
type decoder struct {
	reader  io.Reader
	hasher  crypto.Hash
	hash    hash.Hash
	version uint32

	objCount     int64
	packChecksum plumbing.ObjectID
	out          chan<- uint32
}

// stateFn defines each individual state within the state machine that
// represents a revfile.
type stateFn func(*decoder) (stateFn, error)

// Decode reads a rev file and sends index positions to out.
// The caller must not close out; Decode closes it when done.
// This function is safe to call concurrently with different parameters.
func Decode(r io.Reader, objCount int64, packChecksum plumbing.ObjectID, out chan<- uint32) error {
	if r == nil {
		return fmt.Errorf("%w: nil reader", ErrMalformedRevFile)
	}

	if out == nil {
		return errors.New("nil channel")
	}

	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}

	d := &decoder{
		reader:       br,
		objCount:     objCount,
		packChecksum: packChecksum,
		out:          out,
	}

	defer close(d.out)

	for state := readMagicNumber; state != nil; {
		var err error
		state, err = state(d)
		if err != nil {
			return err
		}
	}
	return nil
}

func readMagicNumber(d *decoder) (stateFn, error) {
	h := make([]byte, 4)
	if _, err := io.ReadFull(d.reader, h); err != nil {
		return nil, err
	}

	if !bytes.Equal(h, revHeader) {
		return nil, ErrMalformedRevFile
	}

	return readVersion, nil
}

func readVersion(d *decoder) (stateFn, error) {
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

func readHashFunction(d *decoder) (stateFn, error) {
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

func readEntries(d *decoder) (stateFn, error) {
	if d.objCount == 0 {
		return nil, ErrEmptyReverseIndex
	}

	var i int64
	for i = 0; i < d.objCount; i++ {
		idx, err := binary.ReadUint32(d.reader)
		if err == io.EOF {
			return nil, fmt.Errorf("%w: unexpected EOF at object %d", ErrMalformedRevFile, i)
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

func readPackChecksum(d *decoder) (stateFn, error) {
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

func readRevChecksum(d *decoder) (stateFn, error) {
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

	// Check for unexpected trailing data
	var buf [1]byte
	extra, err := d.reader.Read(buf[:])
	if extra > 0 {
		return nil, fmt.Errorf("%w: expected EOF", ErrMalformedRevFile)
	}
	if err != io.EOF {
		return nil, err
	}

	return nil, nil
}
