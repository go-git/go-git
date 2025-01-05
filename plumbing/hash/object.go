package hash

import "io"

// ObjectID represents a calculated hash, which is immutable by nature.
type ObjectID interface {
	// Size returns the length of the resulting hash.
	Size() int
	// IsZero returns true if the objectID only contains 0s.
	IsZero() bool
	// Compare compares the hash's sum with a slice of bytes.
	Compare([]byte) int
	// String returns the hexadecimal representation of the hash's sum.
	String() string
	// Bytes returns a slice of bytes containing the ObjectID.
	Bytes() []byte
	// HasPrefix verifies whether the objectID start with a given prefix.
	HasPrefix([]byte) bool
}

// LazyObjectID represents an object hash which may not be known at the time
// the object is created. Or an objectID which changes during its lifetime.
type LazyObjectID interface {
	ObjectID

	io.Writer
	FromReaderAt(r io.ReaderAt, off int64) (int, error)
	FromReader(r io.Reader) (int, error)
}
