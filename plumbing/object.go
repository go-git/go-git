// Package plumbing implements the core interfaces and structs used by go-git.
package plumbing

import (
	"errors"
	"io"
)

var (
	// ErrObjectNotFound is returned when an object is not found.
	ErrObjectNotFound = errors.New("object not found")
	// ErrInvalidType is returned when an invalid object type is provided.
	ErrInvalidType = errors.New("invalid object type")
)

// EncodedObject is a generic representation of any git object.
type EncodedObject interface {
	Hash() Hash
	Type() ObjectType
	SetType(ObjectType)
	Size() int64
	SetSize(int64)
	Reader() (io.ReadCloser, error)
	Writer() (io.WriteCloser, error)
}

// DeltaObject is an EncodedObject representing a delta.
type DeltaObject interface {
	EncodedObject
	// BaseHash returns the hash of the object used as base for this delta.
	BaseHash() Hash
	// ActualHash returns the hash of the object after applying the delta.
	ActualHash() Hash
	// Size returns the size of the object after applying the delta.
	ActualSize() int64
}

// ObjectType internal object type
// Integer values from 0 to 7 map to those exposed by git.
// AnyObject is used to represent any from 0 to 7.
type ObjectType int8

const (
	// InvalidObject represents an invalid object type.
	InvalidObject ObjectType = 0
	// CommitObject is a git commit object.
	CommitObject ObjectType = 1
	// TreeObject is a git tree object.
	TreeObject ObjectType = 2
	// BlobObject is a git blob object.
	BlobObject ObjectType = 3
	// TagObject is a git tag object.
	TagObject ObjectType = 4
	// OFSDeltaObject is an offset delta object type (5 reserved for future expansion).
	OFSDeltaObject ObjectType = 6
	// REFDeltaObject is a reference delta object type.
	REFDeltaObject ObjectType = 7

	// AnyObject is used to represent any object type.
	AnyObject ObjectType = -127
)

func (t ObjectType) String() string {
	switch t {
	case CommitObject:
		return "commit"
	case TreeObject:
		return "tree"
	case BlobObject:
		return "blob"
	case TagObject:
		return "tag"
	case OFSDeltaObject:
		return "ofs-delta"
	case REFDeltaObject:
		return "ref-delta"
	case AnyObject:
		return "any"
	default:
		return "unknown"
	}
}

// Bytes returns the byte representation of the ObjectType.
func (t ObjectType) Bytes() []byte {
	return []byte(t.String())
}

// Valid returns true if t is a valid ObjectType.
func (t ObjectType) Valid() bool {
	return t >= CommitObject && t <= REFDeltaObject
}

// IsDelta returns true for any ObjectType that represents a delta (i.e.
// REFDeltaObject or OFSDeltaObject).
func (t ObjectType) IsDelta() bool {
	return t == REFDeltaObject || t == OFSDeltaObject
}

// ParseObjectType parses a string representation of ObjectType. It returns an
// error on parse failure.
func ParseObjectType(value string) (typ ObjectType, err error) {
	switch value {
	case "commit":
		typ = CommitObject
	case "tree":
		typ = TreeObject
	case "blob":
		typ = BlobObject
	case "tag":
		typ = TagObject
	case "ofs-delta":
		typ = OFSDeltaObject
	case "ref-delta":
		typ = REFDeltaObject
	default:
		err = ErrInvalidType
	}
	return typ, err
}
