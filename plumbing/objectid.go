package plumbing

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

var (
	empty = make([]byte, hash.SHA256Size)
)

// FromHex parses a hexadecimal string and returns an ObjectID
// and a boolean confirming whether the operation was successful.
// The object format is inferred from the length of the input.
//
// For backwards compatibility, partial hashes will be handled as
// being SHA1.
func FromHex(in string) (ObjectID, bool) {
	var id ObjectID

	switch len(in) {
	case hash.SHA256HexSize:
		id.format = format.SHA256
	default:
		id.format = format.SHA1
	}

	out, err := hex.DecodeString(in)
	if err != nil {
		return id, false
	}

	id.Write(out)
	return id, true
}

// FromBytes creates an ObjectID based off raw bytes.
// The object format is inferred from the length of the input.
//
// If the size of [in] does not match the supported object formats,
// an empty ObjectID will be returned.
// Note that
func FromBytes(in []byte) (ObjectID, bool) {
	var id ObjectID

	switch len(in) {
	case hash.SHA1Size:
		id.format = config.SHA1

	case hash.SHA256Size:
		id.format = config.SHA256

	default:
		return id, false
	}

	copy(id.hash[:], in)
	return id, true
}

// ObjectID represents the ID of a Git object. The object data is kept
// in its hexadecimal form.
type ObjectID struct {
	hash   [hash.SHA256Size]byte
	format config.ObjectFormat
}

func (s ObjectID) HexSize() int {
	return s.Size() * 2
}

// Size returns the length of the resulting hash.
func (s ObjectID) Size() int {
	if s.format == config.SHA256 {
		return hash.SHA256Size
	}
	return hash.SHA1Size
}

// Compare compares the hash's sum with a slice of bytes.
func (s ObjectID) Compare(b []byte) int {
	return bytes.Compare(s.hash[:s.Size()], b)
}

func (s ObjectID) Equal(in ObjectID) bool {
	return bytes.Equal(s.hash[:], in.hash[:])
}

// Bytes returns the slice of bytes containing the hash.
func (s ObjectID) Bytes() []byte {
	if len(s.hash) == 0 {
		v := make([]byte, s.Size())
		return v
	}
	return s.hash[:s.Size()]
}

func (s ObjectID) HasPrefix(prefix []byte) bool {
	return bytes.HasPrefix(s.hash[:s.Size()], prefix)
}

// IsZero returns true if the hash is zero.
func (s ObjectID) IsZero() bool {
	return bytes.Equal(s.hash[:], empty)
}

// String returns the hexadecimal representation of the ObjectID.
func (s ObjectID) String() string {
	val := s.hash[:s.Size()]
	return hex.EncodeToString(val)
}

func (s *ObjectID) Write(in []byte) (int, error) {
	if s.format == "" {
		s.format = config.SHA1
	}

	n := copy(s.hash[:], in[:])
	return n, nil
}

// ReadFrom loads the ObjectID from [r].
func (s *ObjectID) ReadFrom(r io.Reader) (int64, error) {
	if s.format == "" {
		s.format = config.SHA1
	}

	err := binary.Read(r, binary.BigEndian, s.hash[:s.Size()])
	if err != nil {
		return 0, fmt.Errorf("read hash from binary: %w", err)
	}
	return int64(s.Size()), nil
}

func (s *ObjectID) WriteTo(w io.Writer) (int64, error) {
	err := binary.Write(w, binary.BigEndian, s.hash[:s.Size()])
	if err != nil {
		return 0, err
	}
	return int64(s.Size()), nil
}

func (s *ObjectID) ResetBySize(idSize int) {
	if idSize == hash.SHA256Size {
		s.format = config.SHA256
	} else {
		s.format = config.SHA1
	}
	copy(s.hash[:], s.hash[:0])
}
