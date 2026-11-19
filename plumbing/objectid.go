package plumbing

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"

	githash "github.com/go-git/go-git/v6/plumbing/hash"
)

var empty = make([]byte, githash.SHA256Size)

// FromHex parses a hexadecimal string and returns an ObjectID
// and a boolean confirming whether the operation was successful.
// The object format is inferred from the length of the input, which
// must be exactly that of a SHA-1 or a SHA-256 id.
func FromHex(in string) (ObjectID, bool) {
	if len(in) != githash.SHA1HexSize && len(in) != githash.SHA256HexSize {
		return ObjectID{}, false
	}
	return fromHexLax(in)
}

// fromHexLax parses a hexadecimal string of any length. Input of any
// length other than a SHA-256 id is handled as SHA-1. Shorter input
// is zero-padded and longer input is truncated. It keeps the behaviour
// [NewHash] has always had.
func fromHexLax(in string) (ObjectID, bool) {
	var id ObjectID

	if len(in) == githash.SHA256HexSize {
		id.format = githash.SHA256
	}

	out, err := hex.DecodeString(in)
	if err != nil {
		return id, false
	}

	_, _ = id.Write(out)
	return id, true
}

// FromBytes creates an ObjectID based off its hex representation in bytes.
// The object format is inferred from the length of the input.
//
// If the size of [in] does not match the supported object formats,
// an empty ObjectID will be returned.
func FromBytes(in []byte) (ObjectID, bool) {
	var id ObjectID

	switch len(in) {
	case githash.SHA1Size:
		id.format = githash.UnsetObjectFormat

	case githash.SHA256Size:
		id.format = githash.SHA256

	default:
		return id, false
	}

	copy(id.hash[:], in)
	return id, true
}

// ZeroID returns the zero ObjectID for the object format [f]. Unknown
// formats yield the SHA-1 zero, which is [ZeroHash].
func ZeroID(f githash.ObjectFormat) ObjectID {
	if f == githash.SHA256 {
		return ObjectID{format: githash.SHA256}
	}
	return ObjectID{}
}

// ObjectID represents the ID of a Git object. The object data is kept
// in its hexadecimal form.
//
// Every id has exactly one representation regardless of how it was
// produced (parsed, hashed, decoded or read), so ==, [ObjectID.Equal]
// and map keys always agree. SHA-1 ids never carry an explicit format,
// which makes the zero value equal to [ZeroHash]. SHA-256 ids always
// carry their format. Bytes past the format's size are always zero.
// Ids of different formats are never equal, including their zero ids.
// Use [ObjectID.IsZero] to test for a zero id of any format.
type ObjectID struct {
	hash   [githash.SHA256Size]byte
	format githash.ObjectFormat
}

// Format returns the object format of the ObjectID. Ids that carry no
// explicit format are SHA-1.
func (s ObjectID) Format() githash.ObjectFormat {
	if s.format == githash.SHA256 {
		return githash.SHA256
	}
	return githash.SHA1
}

// HexSize returns the size for the hex representation of the current
// object.
func (s ObjectID) HexSize() int {
	return s.format.HexSize()
}

// Size returns the length of the resulting hash.
func (s ObjectID) Size() int {
	return s.format.Size()
}

// Compare compares the hash's sum with a slice of bytes.
func (s ObjectID) Compare(b []byte) int {
	return bytes.Compare(s.hash[:s.Size()], b)
}

// Equal returns true if [in] equals the current object. Ids of
// different object formats are never equal. It is equivalent to ==.
func (s ObjectID) Equal(in ObjectID) bool {
	return s == in
}

// Bytes returns the slice of bytes representing the hash in hexadecimal.
func (s ObjectID) Bytes() []byte {
	if len(s.hash) == 0 {
		v := make([]byte, s.Size())
		return v
	}
	return s.hash[:s.Size()]
}

// HasPrefix checks whether the ObjectID starts with [prefix].
func (s ObjectID) HasPrefix(prefix []byte) bool {
	return bytes.HasPrefix(s.hash[:s.Size()], prefix)
}

// IsZero returns true if the hash is zero, whatever its object format.
// It is the only format-agnostic zero test, as == [ZeroHash] only
// matches the SHA-1 zero id.
func (s ObjectID) IsZero() bool {
	return bytes.Equal(s.hash[:], empty)
}

// String returns the hexadecimal representation of the ObjectID.
func (s ObjectID) String() string {
	val := s.hash[:s.Size()]
	return hex.EncodeToString(val)
}

// Write writes the hexadecimal representation of the ObjectID from [in]
// directly into the current object. At most the ObjectID's current size
// is written, and any bytes in [in] past it are ignored.
func (s *ObjectID) Write(in []byte) (int, error) {
	n := copy(s.hash[:s.Size()], in)
	return n, nil
}

// ReadFrom reads the raw bytes of the ObjectID from reader [r].
// The number of bytes read is determined by the ObjectID's current size.
func (s *ObjectID) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.ReadFull(r, s.hash[:s.Size()])
	if err != nil {
		// Clear partial read so the hash remains zero on error.
		s.ResetBySize(s.Size())
		return 0, err
	}
	return int64(n), nil
}

// WriteTo writes the Big Endian representation of the ObjectID
// into the writer [w].
func (s *ObjectID) WriteTo(w io.Writer) (int64, error) {
	err := binary.Write(w, binary.BigEndian, s.hash[:s.Size()])
	if err != nil {
		return 0, err
	}
	return int64(s.Size()), nil
}

// ResetBySize resets the current ObjectID. It sets the
// underlying format based on the [idSize], which defaults
// to SHA1 for backwards compatibility.
//
// This enable complete reuse of this object without needing
// to create a new instance of ObjectID.
func (s *ObjectID) ResetBySize(idSize int) {
	if idSize == githash.SHA256Size {
		s.format = githash.SHA256
	} else {
		s.format = githash.UnsetObjectFormat
	}
	clear(s.hash[:])
}
