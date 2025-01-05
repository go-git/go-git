package packfile

import (
	"bytes"
	"io"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-billy/v5/memfs"
	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type EncoderFixtureSuite struct {
	fixtures.Suite
}

type EncoderSuite struct {
	suite.Suite
	EncoderFixtureSuite
	buf   *bytes.Buffer
	store *memory.Storage
	enc   *Encoder
}

func TestEncoderSuite(t *testing.T) {
	suite.Run(t, new(EncoderSuite))
}

func (s *EncoderSuite) SetupTest() {
	s.buf = bytes.NewBuffer(nil)
	s.store = memory.NewStorage()
	s.enc = NewEncoder(s.buf, s.store, false)
}

func (s *EncoderSuite) TestCorrectPackHeader() {
	h, err := s.enc.Encode([]plumbing.Hash{}, 10)
	s.NoError(err)

	// PACK + VERSION + OBJECTS + HASH
	expectedResult := []byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 0}
	expectedResult = append(expectedResult, h.Bytes()...)

	result := s.buf.Bytes()

	s.Equal(expectedResult, result)
}

func (s *EncoderSuite) TestCorrectPackWithOneEmptyObject() {
	o := &plumbing.MemoryObject{}
	o.SetType(plumbing.CommitObject)
	o.SetSize(0)
	_, err := s.store.SetEncodedObject(o)
	s.NoError(err)

	h, err := s.enc.Encode([]plumbing.Hash{o.Hash()}, 10)
	s.NoError(err)

	// PACK + VERSION(2) + OBJECT NUMBER(1)
	expectedResult := []byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 1}
	// OBJECT HEADER(TYPE + SIZE)= 0001 0000
	expectedResult = append(expectedResult, []byte{16}...)

	// Zlib header
	expectedResult = append(expectedResult,
		[]byte{120, 156, 1, 0, 0, 255, 255, 0, 0, 0, 1}...)

	// + HASH
	expectedResult = append(expectedResult, h.Bytes()...)

	result := s.buf.Bytes()

	s.Equal(expectedResult, result)
}

func (s *EncoderSuite) TestMaxObjectSize() {
	o := s.store.NewEncodedObject()
	o.SetSize(9223372036854775807)
	o.SetType(plumbing.CommitObject)
	_, err := s.store.SetEncodedObject(o)
	s.NoError(err)
	hash, err := s.enc.Encode([]plumbing.Hash{o.Hash()}, 10)
	s.NoError(err)
	s.NotEqual(true, hash.IsZero())
}

func (s *EncoderSuite) TestHashNotFound() {
	h, err := s.enc.Encode([]plumbing.Hash{plumbing.NewHash("BAD")}, 10)
	s.Equal(plumbing.ZeroHash, h)
	s.NotNil(err)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *EncoderSuite) TestDecodeEncodeWithDeltaDecodeREF() {
	s.enc = NewEncoder(s.buf, s.store, true)
	s.simpleDeltaTest()
}

func (s *EncoderSuite) TestDecodeEncodeWithDeltaDecodeOFS() {
	s.enc = NewEncoder(s.buf, s.store, false)
	s.simpleDeltaTest()
}

func (s *EncoderSuite) TestDecodeEncodeWithDeltasDecodeREF() {
	s.enc = NewEncoder(s.buf, s.store, true)
	s.deltaOverDeltaTest()
}

func (s *EncoderSuite) TestDecodeEncodeWithDeltasDecodeOFS() {
	s.enc = NewEncoder(s.buf, s.store, false)
	s.deltaOverDeltaTest()
}

func (s *EncoderSuite) TestDecodeEncodeWithCycleREF() {
	s.enc = NewEncoder(s.buf, s.store, true)
	s.deltaOverDeltaCyclicTest()
}

func (s *EncoderSuite) TestDecodeEncodeWithCycleOFS() {
	s.enc = NewEncoder(s.buf, s.store, false)
	s.deltaOverDeltaCyclicTest()
}

func (s *EncoderSuite) simpleDeltaTest() {
	srcObject := newObject(plumbing.BlobObject, []byte("0"))
	targetObject := newObject(plumbing.BlobObject, []byte("01"))

	deltaObject, err := GetDelta(srcObject, targetObject)
	s.NoError(err)

	srcToPack := newObjectToPack(srcObject)
	encHash, err := s.enc.encode([]*ObjectToPack{
		srcToPack,
		newDeltaObjectToPack(srcToPack, targetObject, deltaObject),
	})
	s.NoError(err)

	p, cleanup := packfileFromReader(s, s.buf)
	defer cleanup()
	decHash, err := p.ID()
	s.NoError(err)

	s.Equal(decHash, encHash)

	decSrc, err := p.Get(srcObject.Hash())
	s.NoError(err)
	objectsEqual(s, decSrc, srcObject)

	decTarget, err := p.Get(targetObject.Hash())
	s.NoError(err)
	objectsEqual(s, decTarget, targetObject)
}

func (s *EncoderSuite) deltaOverDeltaTest() {
	srcObject := newObject(plumbing.BlobObject, []byte("0"))
	targetObject := newObject(plumbing.BlobObject, []byte("01"))
	otherTargetObject := newObject(plumbing.BlobObject, []byte("011111"))

	deltaObject, err := GetDelta(srcObject, targetObject)
	s.NoError(err)
	s.NotEqual(plumbing.ZeroHash, deltaObject.Hash())

	otherDeltaObject, err := GetDelta(targetObject, otherTargetObject)
	s.NoError(err)
	s.NotEqual(plumbing.ZeroHash, otherDeltaObject.Hash())

	srcToPack := newObjectToPack(srcObject)
	targetToPack := newObjectToPack(targetObject)
	encHash, err := s.enc.encode([]*ObjectToPack{
		targetToPack,
		srcToPack,
		newDeltaObjectToPack(srcToPack, targetObject, deltaObject),
		newDeltaObjectToPack(targetToPack, otherTargetObject, otherDeltaObject),
	})
	s.NoError(err)

	p, cleanup := packfileFromReader(s, s.buf)
	defer cleanup()
	decHash, err := p.ID()
	s.NoError(err)

	s.Equal(decHash, encHash)

	decSrc, err := p.Get(srcObject.Hash())
	s.NoError(err)
	objectsEqual(s, decSrc, srcObject)

	decTarget, err := p.Get(targetObject.Hash())
	s.NoError(err)
	objectsEqual(s, decTarget, targetObject)

	decOtherTarget, err := p.Get(otherTargetObject.Hash())
	s.NoError(err)
	objectsEqual(s, decOtherTarget, otherTargetObject)
}

func (s *EncoderSuite) deltaOverDeltaCyclicTest() {
	o1 := newObject(plumbing.BlobObject, []byte("0"))
	o2 := newObject(plumbing.BlobObject, []byte("01"))
	o3 := newObject(plumbing.BlobObject, []byte("011111"))
	o4 := newObject(plumbing.BlobObject, []byte("01111100000"))

	_, err := s.store.SetEncodedObject(o1)
	s.NoError(err)
	_, err = s.store.SetEncodedObject(o2)
	s.NoError(err)
	_, err = s.store.SetEncodedObject(o3)
	s.NoError(err)
	_, err = s.store.SetEncodedObject(o4)
	s.NoError(err)

	d2, err := GetDelta(o1, o2)
	s.NoError(err)

	d3, err := GetDelta(o4, o3)
	s.NoError(err)

	d4, err := GetDelta(o3, o4)
	s.NoError(err)

	po1 := newObjectToPack(o1)
	pd2 := newDeltaObjectToPack(po1, o2, d2)
	pd3 := newObjectToPack(o3)
	pd4 := newObjectToPack(o4)

	pd3.SetDelta(pd4, d3)
	pd4.SetDelta(pd3, d4)

	// SetOriginal is used by delta selector when generating ObjectToPack.
	// It also fills type, hash and size values to be used when Original
	// is nil.
	po1.SetOriginal(po1.Original)
	pd2.SetOriginal(pd2.Original)
	pd2.CleanOriginal()

	pd3.SetOriginal(pd3.Original)
	pd3.CleanOriginal()

	pd4.SetOriginal(pd4.Original)

	encHash, err := s.enc.encode([]*ObjectToPack{
		po1,
		pd2,
		pd3,
		pd4,
	})
	s.NoError(err)

	p, cleanup := packfileFromReader(s, s.buf)
	defer cleanup()
	decHash, err := p.ID()
	s.NoError(err)

	s.Equal(decHash, encHash)

	decSrc, err := p.Get(o1.Hash())
	s.NoError(err)
	objectsEqual(s, decSrc, o1)

	decTarget, err := p.Get(o2.Hash())
	s.NoError(err)
	objectsEqual(s, decTarget, o2)

	decOtherTarget, err := p.Get(o3.Hash())
	s.NoError(err)
	objectsEqual(s, decOtherTarget, o3)

	decAnotherTarget, err := p.Get(o4.Hash())
	s.NoError(err)
	objectsEqual(s, decAnotherTarget, o4)
}

func objectsEqual(s *EncoderSuite, o1, o2 plumbing.EncodedObject) {
	s.Equal(o2.Type(), o1.Type())
	s.Equal(o2.Hash(), o1.Hash())
	s.Equal(o2.Size(), o1.Size())

	r1, err := o1.Reader()
	s.NoError(err)

	b1, err := io.ReadAll(r1)
	s.NoError(err)

	r2, err := o2.Reader()
	s.NoError(err)

	b2, err := io.ReadAll(r2)
	s.NoError(err)

	s.Equal(0, bytes.Compare(b1, b2))

	err = r2.Close()
	s.NoError(err)

	err = r1.Close()
	s.NoError(err)
}

func packfileFromReader(s *EncoderSuite, buf *bytes.Buffer) (*Packfile, func()) {
	fs := memfs.New()
	file, err := fs.Create("packfile")
	s.NoError(err)

	_, err = file.Write(buf.Bytes())
	s.NoError(err)

	_, err = file.Seek(0, io.SeekStart)
	s.NoError(err)

	scanner := NewScanner(file)

	w := new(idxfile.Writer)
	p := NewParser(scanner, WithScannerObservers(w))

	_, err = p.Parse()
	s.NoError(err)

	index, err := w.Index()
	s.NoError(err)

	_, err = file.Seek(0, io.SeekStart)
	s.NoError(err)

	return NewPackfile(file, WithIdx(index), WithFs(fs)), func() {
		s.NoError(file.Close())
	}
}
