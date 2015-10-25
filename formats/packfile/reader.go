package packfile

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/klauspost/compress/zlib"
)

type Format int

const (
	DefaultMaxObjectsLimit = 1 << 20
	DefaultMaxObjectSize   = 1 << 32 // 4GB

	VersionSupported        = 2
	UnknownFormat    Format = 0
	OFSDeltaFormat   Format = 1
	REFDeltaFormat   Format = 2
)

type PackfileReader struct {
	// MaxObjectsLimit is the limit of objects to be load in the packfile, if
	// a packfile excess this number an error is throw, the default value
	// is defined by DefaultMaxObjectsLimit, usually the default limit is more
	// than enough to work with any repository, working extremly big repositories
	// where the number of object is bigger the memory can be exhausted.
	MaxObjectsLimit uint32

	// MaxObjectSize is the maximum size in bytes, reading objects with a bigger
	// size cause a error. The default value is defined by DefaultMaxObjectSize
	MaxObjectSize uint64

	// Format specifies if we are using ref-delta's or ofs-delta's, choosing the
	// correct format the memory usage is optimized
	// https://github.com/git/git/blob/8d530c4d64ffcc853889f7b385f554d53db375ed/Documentation/technical/protocol-capabilities.txt#L154
	Format Format

	r       *trackingReader
	objects map[Hash]*RAWObject
	offsets map[int]*RAWObject
}

func NewPackfileReader(r io.Reader, fn ContentCallback) (*PackfileReader, error) {
	return &PackfileReader{
		MaxObjectsLimit: DefaultMaxObjectsLimit,
		MaxObjectSize:   DefaultMaxObjectSize,

		r:       &trackingReader{r: r},
		objects: make(map[Hash]*RAWObject, 0),
		offsets: make(map[int]*RAWObject, 0),
	}, nil
}

func (pr *PackfileReader) Read() (chan *RAWObject, error) {
	if err := pr.validateHeader(); err != nil {
		if err == io.EOF {
			// This is an empty repo. It's OK.
			return nil, nil
		}

		return nil, err
	}

	version, err := pr.readInt32()
	if err != nil {
		return nil, err
	}

	if version > VersionSupported {
		return nil, NewError("unsupported packfile version %d", version)
	}

	count, err := pr.readInt32()
	if err != nil {
		return nil, err
	}

	if count > pr.MaxObjectsLimit {
		return nil, NewError("too many objects %d, limit is %d", count, pr.MaxObjectsLimit)
	}

	ch := make(chan *RAWObject, 1)
	go pr.readObjects(ch, count)

	// packfile.Size = int64(pr.r.Pos())

	return ch, nil
}

func (pr *PackfileReader) validateHeader() error {
	var header = make([]byte, 4)
	if _, err := pr.r.Read(header); err != nil {
		return err
	}

	if !bytes.Equal(header, []byte{'P', 'A', 'C', 'K'}) {
		return NewError("Pack file does not start with 'PACK'")
	}

	return nil
}

func (pr *PackfileReader) readInt32() (uint32, error) {
	var value uint32
	if err := binary.Read(pr.r, binary.BigEndian, &value); err != nil {
		return 0, err
	}

	return value, nil
}

func (pr *PackfileReader) readObjects(ch chan *RAWObject, count uint32) error {
	// This code has 50-80 µs of overhead per object not counting zlib inflation.
	// Together with zlib inflation, it's 400-410 µs for small objects.
	// That's 1 sec for ~2450 objects, ~4.20 MB, or ~250 ms per MB,
	// of which 12-20 % is _not_ zlib inflation (ie. is our code).
	defer func() {
		close(ch)
	}()

	for i := 0; i < int(count); i++ {
		var pos = pr.Pos()
		obj, err := pr.newRAWObject()
		if err != nil && err != io.EOF {
			fmt.Println(err)
			return err
		}

		if pr.Format == UnknownFormat || pr.Format == OFSDeltaFormat {
			pr.offsets[pos] = obj
		}

		if pr.Format == UnknownFormat || pr.Format == REFDeltaFormat {
			pr.objects[obj.Hash] = obj
		}

		ch <- obj

		if err == io.EOF {
			break
		}
	}

	return nil
}

func (pr *PackfileReader) Pos() int { return pr.r.Pos() }

func (pr *PackfileReader) newRAWObject() (*RAWObject, error) {
	raw := &RAWObject{}
	steps := 0

	var buf [1]byte
	if _, err := pr.r.Read(buf[:]); err != nil {
		return nil, err
	}

	raw.Type = ObjectType((buf[0] >> 4) & 7)
	raw.Size = uint64(buf[0] & 15)
	steps++ // byte we just read to get `o.typ` and `o.size`

	var shift uint = 4
	for buf[0]&0x80 == 0x80 {
		if _, err := pr.r.Read(buf[:]); err != nil {
			return nil, err
		}

		raw.Size += uint64(buf[0]&0x7f) << shift
		steps++ // byte we just read to update `o.size`
		shift += 7
	}

	var err error
	switch raw.Type {
	case REFDeltaObject:
		err = pr.readREFDelta(raw)
	case OFSDeltaObject:
		err = pr.readOFSDelta(raw, steps)
	case CommitObject, TreeObject, BlobObject, TagObject:
		err = pr.readObject(raw)
	default:
		err = NewError("Invalid git object tag %q", raw.Type)
	}

	return raw, err
}

func (pr *PackfileReader) readREFDelta(raw *RAWObject) error {
	var ref Hash
	if _, err := pr.r.Read(ref[:]); err != nil {
		return err
	}

	buf, err := pr.inflate(raw.Size)
	if err != nil {
		return err
	}

	referenced, ok := pr.objects[ref]
	if !ok {
		fmt.Println("not found", ref)
	} else {
		patched := PatchDelta(referenced.Bytes, buf[:])
		if patched == nil {
			return NewError("error while patching %x", ref)
		}

		raw.Type = referenced.Type
		raw.Bytes = patched
		raw.Size = uint64(len(patched))
		raw.Hash = ComputeHash(raw.Type, raw.Bytes)
	}

	return nil
}

func (pr *PackfileReader) readOFSDelta(raw *RAWObject, steps int) error {
	var pos = pr.Pos()

	// read negative offset
	offset, err := decodeOffset(pr.r, steps)
	if err != nil {
		return err
	}

	buf, err := pr.inflate(raw.Size)
	if err != nil {
		return err
	}

	ref, ok := pr.offsets[pos+offset]
	if !ok {
		return NewError("can't find a pack entry at %d", pos+offset)
	}

	patched := PatchDelta(ref.Bytes, buf)
	if patched == nil {
		return NewError("error while patching %q", ref)
	}

	raw.Type = ref.Type
	raw.Bytes = patched
	raw.Size = uint64(len(patched))
	raw.Hash = ComputeHash(raw.Type, raw.Bytes)

	return nil
}

func (pr *PackfileReader) readObject(raw *RAWObject) error {
	buf, err := pr.inflate(raw.Size)
	if err != nil {
		return err
	}

	raw.Bytes = buf
	raw.Hash = ComputeHash(raw.Type, raw.Bytes)

	return nil
}

func (pr *PackfileReader) inflate(size uint64) ([]byte, error) {
	zr, err := zlib.NewReader(pr.r)
	if err != nil {
		if err == zlib.ErrHeader {
			return nil, zlib.ErrHeader
		}

		return nil, NewError("error opening packfile's object zlib: %v", err)
	}

	defer zr.Close()

	if size > pr.MaxObjectSize {
		return nil, NewError("the object size %q exceeed the allowed limit: %q",
			size, pr.MaxObjectSize)
	}

	var buf bytes.Buffer
	io.Copy(&buf, zr) // also: io.CopyN(&buf, zr, int64(o.size))

	if buf.Len() != int(size) {
		return nil, NewError(
			"inflated size mismatch, expected %d, got %d", size, buf.Len())
	}

	return buf.Bytes(), nil
}

type ReaderError struct {
	Msg string // description of error
}

func NewError(format string, args ...interface{}) error {
	return &ReaderError{Msg: fmt.Sprintf(format, args...)}
}

func (e *ReaderError) Error() string { return e.Msg }
