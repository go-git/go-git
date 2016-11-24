package packfile

import (
	"bytes"

	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
)

type EncoderSuite struct {
	fixtures.Suite
	buf   *bytes.Buffer
	store *memory.Storage
	enc   *Encoder
}

var _ = Suite(&EncoderSuite{})

func (s *EncoderSuite) SetUpTest(c *C) {
	s.buf = bytes.NewBuffer(nil)
	s.store = memory.NewStorage()
	s.enc = NewEncoder(s.buf, s.store)
}

func (s *EncoderSuite) TestCorrectPackHeader(c *C) {
	hash, err := s.enc.Encode([]plumbing.Hash{})
	c.Assert(err, IsNil)

	hb := [20]byte(hash)

	// PACK + VERSION + OBJECTS + HASH
	expectedResult := []byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 0}
	expectedResult = append(expectedResult, hb[:]...)

	result := s.buf.Bytes()

	c.Assert(result, DeepEquals, expectedResult)
}

func (s *EncoderSuite) TestCorrectPackWithOneEmptyObject(c *C) {
	o := &plumbing.MemoryObject{}
	o.SetType(plumbing.CommitObject)
	o.SetSize(0)
	_, err := s.store.SetObject(o)
	c.Assert(err, IsNil)

	hash, err := s.enc.Encode([]plumbing.Hash{o.Hash()})
	c.Assert(err, IsNil)

	// PACK + VERSION(2) + OBJECT NUMBER(1)
	expectedResult := []byte{'P', 'A', 'C', 'K', 0, 0, 0, 2, 0, 0, 0, 1}
	// OBJECT HEADER(TYPE + SIZE)= 0001 0000
	expectedResult = append(expectedResult, []byte{16}...)

	// Zlib header
	expectedResult = append(expectedResult,
		[]byte{120, 156, 1, 0, 0, 255, 255, 0, 0, 0, 1}...)

	// + HASH
	hb := [20]byte(hash)
	expectedResult = append(expectedResult, hb[:]...)

	result := s.buf.Bytes()

	c.Assert(result, DeepEquals, expectedResult)
}

func (s *EncoderSuite) TestMaxObjectSize(c *C) {
	o := s.store.NewObject()
	o.SetSize(9223372036854775807)
	o.SetType(plumbing.CommitObject)
	_, err := s.store.SetObject(o)
	c.Assert(err, IsNil)
	hash, err := s.enc.Encode([]plumbing.Hash{o.Hash()})
	c.Assert(err, IsNil)
	c.Assert(hash.IsZero(), Not(Equals), true)
}

func (s *EncoderSuite) TestDecodeEncodeDecode(c *C) {
	fixtures.Basic().ByTag("packfile").Test(c, func(f *fixtures.Fixture) {
		scanner := NewScanner(f.Packfile())
		storage := memory.NewStorage()

		d, err := NewDecoder(scanner, storage)
		c.Assert(err, IsNil)

		ch, err := d.Decode()
		c.Assert(err, IsNil)
		c.Assert(ch, Equals, f.PackfileHash)

		commitIter, err := d.o.IterObjects(plumbing.AnyObject)
		c.Assert(err, IsNil)

		objects := []plumbing.Object{}
		hashes := []plumbing.Hash{}
		err = commitIter.ForEach(func(o plumbing.Object) error {
			objects = append(objects, o)
			hash, err := s.store.SetObject(o)
			hashes = append(hashes, hash)

			return err

		})
		c.Assert(err, IsNil)
		_, err = s.enc.Encode(hashes)
		c.Assert(err, IsNil)

		scanner = NewScanner(s.buf)
		storage = memory.NewStorage()
		d, err = NewDecoder(scanner, storage)
		c.Assert(err, IsNil)
		_, err = d.Decode()
		c.Assert(err, IsNil)

		commitIter, err = d.o.IterObjects(plumbing.AnyObject)
		c.Assert(err, IsNil)
		obtainedObjects := []plumbing.Object{}
		err = commitIter.ForEach(func(o plumbing.Object) error {
			obtainedObjects = append(obtainedObjects, o)

			return nil
		})
		c.Assert(err, IsNil)
		c.Assert(len(obtainedObjects), Equals, len(objects))

		equals := 0
		for _, oo := range obtainedObjects {
			for _, o := range objects {
				if o.Hash() == oo.Hash() {
					equals++
				}
			}
		}

		c.Assert(len(obtainedObjects), Equals, equals)
	})
}
