package idxfile

import (
	"bytes"
	"math"
	"sort"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/utils/binary"
)

type object struct {
	hash   plumbing.Hash
	offset int64
	crc    uint32
}

type objects []object

// Writer implements a packfile Observer interface and is used to generate
// indexes.
type Writer struct {
	count    uint32
	checksum plumbing.Hash
	objects  objects
}

// Create index returns a filled MemoryIndex with the information filled by
// the observer callbacks.
func (w *Writer) CreateIndex() (*MemoryIndex, error) {
	idx := new(MemoryIndex)
	sort.Sort(w.objects)

	// unmap all fans by default
	for i := range idx.FanoutMapping {
		idx.FanoutMapping[i] = noMapping
	}

	buf := new(bytes.Buffer)

	last := -1
	bucket := -1
	for i, o := range w.objects {
		fan := o.hash[0]

		// fill the gaps between fans
		for j := last + 1; j < int(fan); j++ {
			idx.Fanout[j] = uint32(i)
		}

		// update the number of objects for this position
		idx.Fanout[fan] = uint32(i + 1)

		// we move from one bucket to another, update counters and allocate
		// memory
		if last != int(fan) {
			bucket++
			idx.FanoutMapping[fan] = bucket
			last = int(fan)

			idx.Names = append(idx.Names, make([]byte, 0))
			idx.Offset32 = append(idx.Offset32, make([]byte, 0))
			idx.Crc32 = append(idx.Crc32, make([]byte, 0))
		}

		idx.Names[bucket] = append(idx.Names[bucket], o.hash[:]...)

		// TODO: implement 64 bit offsets
		if o.offset > math.MaxInt32 {
			panic("64 bit offsets not implemented")
		}

		buf.Truncate(0)
		binary.WriteUint32(buf, uint32(o.offset))
		idx.Offset32[bucket] = append(idx.Offset32[bucket], buf.Bytes()...)

		buf.Truncate(0)
		binary.WriteUint32(buf, uint32(o.crc))
		idx.Crc32[bucket] = append(idx.Crc32[bucket], buf.Bytes()...)
	}

	for j := last + 1; j < 256; j++ {
		idx.Fanout[j] = uint32(len(w.objects))
	}

	idx.PackfileChecksum = w.checksum
	// TODO: fill IdxChecksum

	return idx, nil
}

// Add appends new object data.
func (w *Writer) Add(h plumbing.Hash, pos int64, crc uint32) {
	w.objects = append(w.objects, object{h, pos, crc})
}

// OnHeader implements packfile.Observer interface.
func (w *Writer) OnHeader(count uint32) error {
	w.count = count
	w.objects = make(objects, 0, count)
	return nil
}

// OnInflatedObjectHeader implements packfile.Observer interface.
func (w *Writer) OnInflatedObjectHeader(t plumbing.ObjectType, objSize int64, pos int64) error {
	return nil
}

// OnInflatedObjectContent implements packfile.Observer interface.
func (w *Writer) OnInflatedObjectContent(h plumbing.Hash, pos int64, crc uint32) error {
	w.Add(h, pos, crc)
	return nil
}

// OnFooter implements packfile.Observer interface.
func (w *Writer) OnFooter(h plumbing.Hash) error {
	w.checksum = h
	return nil
}

func (o objects) Len() int {
	return len(o)
}

func (o objects) Less(i int, j int) bool {
	cmp := bytes.Compare(o[i].hash[:], o[j].hash[:])
	return cmp < 0
}

func (o objects) Swap(i int, j int) {
	o[i], o[j] = o[j], o[i]
}
