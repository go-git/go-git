package idxfile

import "gopkg.in/src-d/go-git.v3/core"

const (
	// VersionSupported is the only idx version supported.
	VersionSupported = 2
)

var (
	idxHeader = []byte{255, 't', 'O', 'c'}
)

// An Idxfile represents an idx file in memory.
type Idxfile struct {
	Version          uint32
	Fanout           [255]uint32
	ObjectCount      uint32
	Entries          []Entry
	PackfileChecksum [20]byte
	IdxChecksum      [20]byte
}

// An Entry represents data about an object in the packfile: its hash,
// offset and CRC32 checksum.
type Entry struct {
	Hash   core.Hash
	CRC32  [4]byte
	Offset uint64
}

func (idx *Idxfile) isValid() bool {
	fanout := idx.calculateFanout()
	for k, c := range idx.Fanout {
		if fanout[k] != c {
			return false
		}
	}

	return true
}

func (idx *Idxfile) calculateFanout() [256]uint32 {
	fanout := [256]uint32{}
	var c uint32
	for _, e := range idx.Entries {
		c++
		fanout[e.Hash[0]] = c
	}

	var i uint32
	for k, c := range fanout {
		if c != 0 {
			i = c
		}

		fanout[k] = i
	}

	return fanout
}
