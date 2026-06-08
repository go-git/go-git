package packfile

import (
	"bytes"

	"github.com/go-git/go-git/v6/plumbing"
)

// Version represents the packfile version.
type Version uint32

// Packfile versions.
const (
	V2 Version = 2
)

// Supported returns true if the version is supported.
func (v Version) Supported() bool {
	switch v {
	case V2:
		return true
	default:
		return false
	}
}

// ObjectHeader contains the information related to the object, this information
// is collected from the previous bytes to the content of the object.
type ObjectHeader struct {
	Type            plumbing.ObjectType
	Offset          int64
	ContentOffset   int64
	Size            int64
	Reference       plumbing.Hash
	OffsetReference int64
	Crc32           uint32
	Hash            plumbing.Hash

	content     *bytes.Buffer
	parent      *ObjectHeader
	diskType    plumbing.ObjectType
	externalRef bool

	// chainDepth caches the result of [checkDeltaChainDepth] for
	// this header. A positive value is the number of delta links
	// from this object down to (but not including) the first
	// non-delta base. Zero means either "not yet computed" or
	// "this header is not a delta"; both cases collapse to a
	// constant-time re-check, so the dual meaning is harmless.
	chainDepth int
}

// ID returns the object ID.
func (oh *ObjectHeader) ID() plumbing.Hash {
	return oh.Hash
}

// SectionType represents the type of section in a packfile.
type SectionType int

// Section types.
const (
	HeaderSection SectionType = iota
	ObjectSection
	FooterSection
)

// Header represents the packfile header.
type Header struct {
	Version    Version
	ObjectsQty uint32
}

// PackData represents the data returned by the scanner.
type PackData struct {
	Section      SectionType
	header       Header
	objectHeader ObjectHeader
	checksum     plumbing.Hash
}

// Value returns the value of the PackData based on its section type.
func (p PackData) Value() any {
	switch p.Section {
	case HeaderSection:
		return p.header
	case ObjectSection:
		return p.objectHeader
	case FooterSection:
		return p.checksum
	default:
		return nil
	}
}
