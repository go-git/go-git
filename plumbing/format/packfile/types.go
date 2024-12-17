package packfile

import (
	"bytes"

	"github.com/go-git/go-git/v5/plumbing"
)

type Version uint32

const (
	V2 Version = 2
)

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
	Hash256         *plumbing.Hash256

	content     bytes.Buffer
	parent      *ObjectHeader
	diskType    plumbing.ObjectType
	externalRef bool
}

type SectionType int

const (
	HeaderSection SectionType = iota
	ObjectSection
	FooterSection
)

type Header struct {
	Version    Version
	ObjectsQty uint32
}

type PackData struct {
	Section      SectionType
	header       Header
	objectHeader ObjectHeader
	checksum     plumbing.Hash
}

func (p PackData) Value() interface{} {
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
