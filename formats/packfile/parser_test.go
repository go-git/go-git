package packfile

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/storage/memory"
)

const (
	sigOffset   = 0
	verOffset   = 4
	countOffset = 8
)

type ParserSuite struct {
	fixtures map[string]*fix
}

type fix struct {
	path     string
	parser   *Parser
	seekable io.Seeker
}

func newFix(path string) (*fix, error) {
	fix := new(fix)
	fix.path = path

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	if err = f.Close(); err != nil {
		return nil, err
	}

	seekable := NewSeekable(bytes.NewReader(data))
	fix.seekable = seekable
	fix.parser = NewParser(seekable)

	return fix, nil
}

func (f *fix) seek(o int64) error {
	_, err := f.seekable.Seek(o, os.SEEK_SET)
	return err
}

var _ = Suite(&ParserSuite{})

func (s *ParserSuite) SetUpSuite(c *C) {
	s.fixtures = make(map[string]*fix)
	for _, fixData := range []struct {
		id   string
		path string
	}{
		{"ofs-deltas", "fixtures/alcortesm-binary-relations.pack"},
		{"ref-deltas", "fixtures/git-fixture.ref-delta"},
	} {
		fix, err := newFix(fixData.path)
		c.Assert(err, IsNil,
			Commentf("setting up fixture id %s: %s", fixData.id, err))

		_, ok := s.fixtures[fixData.id]
		c.Assert(ok, Equals, false,
			Commentf("duplicated fixture id: %s", fixData.id))

		s.fixtures[fixData.id] = fix
	}
}

func (s *ParserSuite) TestSignature(c *C) {
	for id, fix := range s.fixtures {
		com := Commentf("fixture id = %s", id)
		err := fix.seek(sigOffset)
		c.Assert(err, IsNil, com)
		p := fix.parser

		sig, err := p.ReadSignature()
		c.Assert(err, IsNil, com)
		c.Assert(p.IsValidSignature(sig), Equals, true, com)
	}
}

func (s *ParserSuite) TestVersion(c *C) {
	for i, test := range [...]struct {
		fixID    string
		expected uint32
	}{
		{
			fixID:    "ofs-deltas",
			expected: uint32(2),
		}, {
			fixID:    "ref-deltas",
			expected: uint32(2),
		},
	} {
		com := Commentf("test %d) fixture id = %s", i, test.fixID)
		fix, ok := s.fixtures[test.fixID]
		c.Assert(ok, Equals, true, com)

		err := fix.seek(verOffset)
		c.Assert(err, IsNil, com)
		p := fix.parser

		v, err := p.ReadVersion()
		c.Assert(err, IsNil, com)
		c.Assert(v, Equals, test.expected, com)
		c.Assert(p.IsSupportedVersion(v), Equals, true, com)
	}
}

func (s *ParserSuite) TestCount(c *C) {
	for i, test := range [...]struct {
		fixID    string
		expected uint32
	}{
		{
			fixID:    "ofs-deltas",
			expected: uint32(0x50),
		}, {
			fixID:    "ref-deltas",
			expected: uint32(0x1c),
		},
	} {
		com := Commentf("test %d) fixture id = %s", i, test.fixID)
		fix, ok := s.fixtures[test.fixID]
		c.Assert(ok, Equals, true, com)

		err := fix.seek(countOffset)
		c.Assert(err, IsNil, com)
		p := fix.parser

		count, err := p.ReadCount()
		c.Assert(err, IsNil, com)
		c.Assert(count, Equals, test.expected, com)
	}
}

func (s *ParserSuite) TestReadObjectTypeAndLength(c *C) {
	for i, test := range [...]struct {
		fixID     string
		offset    int64
		expType   core.ObjectType
		expLength int64
	}{
		{
			fixID:     "ofs-deltas",
			offset:    12,
			expType:   core.CommitObject,
			expLength: 342,
		}, {
			fixID:     "ofs-deltas",
			offset:    1212,
			expType:   core.OFSDeltaObject,
			expLength: 104,
		}, {
			fixID:     "ofs-deltas",
			offset:    3193,
			expType:   core.TreeObject,
			expLength: 226,
		}, {
			fixID:     "ofs-deltas",
			offset:    3639,
			expType:   core.BlobObject,
			expLength: 90,
		}, {
			fixID:     "ofs-deltas",
			offset:    4504,
			expType:   core.BlobObject,
			expLength: 7107,
		}, {
			fixID:     "ref-deltas",
			offset:    84849,
			expType:   core.REFDeltaObject,
			expLength: 6,
		}, {
			fixID:     "ref-deltas",
			offset:    85070,
			expType:   core.REFDeltaObject,
			expLength: 8,
		},
	} {
		com := Commentf("test %d) fixture id = %s", i, test.fixID)
		fix, ok := s.fixtures[test.fixID]
		c.Assert(ok, Equals, true, com)

		err := fix.seek(test.offset)
		c.Assert(err, IsNil, com)
		p := fix.parser

		typ, length, err := p.ReadObjectTypeAndLength()
		c.Assert(err, IsNil, com)
		c.Assert(typ, Equals, test.expType, com)
		c.Assert(length, Equals, test.expLength, com)
	}
}

func (s *ParserSuite) TestReadNonDeltaObjectContent(c *C) {
	for i, test := range [...]struct {
		fixID    string
		offset   int64
		expected []byte
	}{
		{
			fixID:    "ofs-deltas",
			offset:   12,
			expected: []byte("tree 87c87d16e815a43e4e574dd8edd72c5450ac3a8e\nparent a87d72684d1cf68099ce6e9f68689e25e645a14c\nauthor Gorka Guardiola <Gorka Guardiola Múzquiz> 1450265632 +0100\ncommitter Gorka Guardiola <Gorka Guardiola Múzquiz> 1450265632 +0100\n\nChanged example to use dot.\nI did not remove the original files outside of the\ntex, I leave that to alcortes.\n"),
		}, {
			fixID:    "ofs-deltas",
			offset:   1610,
			expected: []byte("tree 4b4f0d9a07109ef0b8a3051138cc20cdb47fa513\nparent b373f85fa2594d7dcd9989f4a5858a81647fb8ea\nauthor Alberto Cortés <alberto@sourced.tech> 1448017995 +0100\ncommitter Alberto Cortés <alberto@sourced.tech> 1448018112 +0100\n\nMove generated images to it own dir (img/)\n\nFixes #1.\n"),
		}, {
			fixID:    "ofs-deltas",
			offset:   10566,
			expected: []byte("40000 map-slice\x00\x00\xce\xfb\x8ew\xf7\xa8\xc6\x1b\x99\xdd$\x91\xffH\xa3\xb0\xb1fy40000 simple-arrays\x00\x9a7\x81\xb7\xfd\x9d(Q\xe2\xa4H\x8c\x03^٬\x90Z\xecy"),
		},
	} {
		com := Commentf("test %d) fixture id = %s", i, test.fixID)
		fix, ok := s.fixtures[test.fixID]
		c.Assert(ok, Equals, true, com)

		err := fix.seek(test.offset)
		c.Assert(err, IsNil, com)
		p := fix.parser

		_, _, err = p.ReadObjectTypeAndLength()
		c.Assert(err, IsNil, com)

		cont, err := p.ReadNonDeltaObjectContent()
		c.Assert(err, IsNil, com)
		c.Assert(cont, DeepEquals, test.expected, com)
	}
}

func (s *ParserSuite) TestReadOFSDeltaObjectContent(c *C) {
	for i, test := range [...]struct {
		fixID      string
		offset     int64
		expOffset  int64
		expType    core.ObjectType
		expContent []byte
	}{
		{
			fixID:      "ofs-deltas",
			offset:     1212,
			expOffset:  -212,
			expType:    core.CommitObject,
			expContent: []byte("tree c4573589ce78ac63769c20742b9a970f6e274a38\nparent 4571a24948494ebe1cb3dc18ca5a9286e79705ae\nauthor Alberto Cortés <alberto@sourced.tech> 1448139640 +0100\ncommitter Alberto Cortés <alberto@sourced.tech> 1448139640 +0100\n\nUpdate reference to binrels module\n"),
		}, {
			fixID:      "ofs-deltas",
			offset:     3514,
			expOffset:  -102,
			expType:    core.TreeObject,
			expContent: []byte("100644 .gitignore\x00\u007fA\x90[Mw\xabJ\x9a-3O\xcd\x0f\xb5\xdbn\x8e!\x83100644 .gitmodules\x00\xd4`\xa8>\x15\xcfd\x05\x81B7_\xc4\v\x04\xa7\xa9A\x85\n100644 Makefile\x00-ҭ\x8c\x14\xdef\x12\xed\x15\x816y\xa6UK\xad\x993\v100644 binary-relations.tex\x00\x802\x05@\x11'^ \xf5<\xf7\xfd\x81%3\xd1o\xa9_$40000 graphs\x00\xdehu\x16\xc6\x0e\\H\x8e\xe9\xa1JIXE\xbaڽg\xc540000 imgs-gen\x00\xeb\"\xddhzg\xa3\x1f\xc8j\xc5\xfc豢\xe9\x96\xce\xce^40000 src\x00\x895\x11t\xff\x86\xa7\xea\xa6\xc0v%\x11E\x10f,ݒ\x1a"),
		}, {
			fixID:      "ofs-deltas",
			offset:     9806,
			expOffset:  -6613,
			expType:    core.TreeObject,
			expContent: []byte("100644 .gitignore\x00\u007fA\x90[Mw\xabJ\x9a-3O\xcd\x0f\xb5\xdbn\x8e!\x83100644 .gitmodules\x00\xd4`\xa8>\x15\xcfd\x05\x81B7_\xc4\v\x04\xa7\xa9A\x85\n100644 Makefile\x00-ҭ\x8c\x14\xdef\x12\xed\x15\x816y\xa6UK\xad\x993\v100644 binary-relations.tex\x00I\x13~\xb8کEU\x9f\x99#\xc4E.\x9d>\uef1e\xad40000 graphs\x00\xb9\x00\xf34\xde\xff\xce@+\xbd\xf8 9\xb8=\xc1\xb9\x00\x84]40000 imgs-gen\x00\xeb\"\xddhzg\xa3\x1f\xc8j\xc5\xfc豢\xe9\x96\xce\xce^40000 src\x00\x895\x11t\xff\x86\xa7\xea\xa6\xc0v%\x11E\x10f,ݒ\x1a"),
		},
	} {
		com := Commentf("test %d) fixture id = %s", i, test.fixID)
		fix, ok := s.fixtures[test.fixID]
		c.Assert(ok, Equals, true, com)

		err := fix.seek(test.offset)
		c.Assert(err, IsNil, com)
		p := fix.parser

		_, _, err = p.ReadObjectTypeAndLength()
		c.Assert(err, IsNil, com)

		beforeJumpSize, err := p.Offset()
		c.Assert(err, IsNil, com)

		jump, err := p.ReadNegativeOffset()
		c.Assert(err, IsNil, com)
		c.Assert(jump, Equals, test.expOffset, com)

		err = fix.seek(beforeJumpSize)
		c.Assert(err, IsNil, com)

		cont, typ, err := p.ReadOFSDeltaObjectContent(test.offset)
		c.Assert(err, IsNil, com)
		c.Assert(typ, Equals, test.expType, com)
		c.Assert(cont, DeepEquals, test.expContent, com)
	}
}

func (s *ParserSuite) TestReadREFDeltaObjectContent(c *C) {
	for i, test := range [...]struct {
		fixID      string
		offset     int64
		deps       map[int64]core.Object
		expHash    core.Hash
		expType    core.ObjectType
		expContent []byte
	}{
		{
			fixID:  "ref-deltas",
			offset: 84849,
			deps: map[int64]core.Object{
				83607: newObject(core.TreeObject, []byte("100644 .gitignore\x002\x85\x8a\xad<8>\xd1\xff\n\x0f\x9b\xdf#\x1dT\xa0\f\x9e\x88100644 CHANGELOG\x00\xd3\xffS\xe0VJ\x9f\x87\xd8\xe8Kn(\xe5\x06\x0eQp\b\xaa100644 LICENSE\x00\xc1\x92\xbdj$\xea\x1a\xb0\x1dxhnA|\x8b\xdc|=\x19\u007f100644 binary.jpg\x00\xd5\xc0\xf4\xab\x81\x18\x97\xca\xdf\x03\xae\xc3X\xae`\xd2\x1f\x91\xc5\r40000 go\x00\xa3\x97q\xa7e\x1f\x97\xfa\xf5\xc7.\b\"M\x85\u007f\xc3Q3\xdb40000 json\x00Z\x87~j\x90j'C\xadnEٜ\x17\x93d*\xaf\x8e\xda40000 php\x00Xj\xf5gл^w\x1eI\xbd\xd9CO^\x0f\xb7m%\xfa40000 vendor\x00\xcfJ\xa3\xb3\x89t\xfb}\x81\xf3g\xc0\x83\x0f}x\xd6Z\xb8k")),
			},
			expHash:    core.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"),
			expType:    core.TreeObject,
			expContent: []byte("100644 .gitignore\x002\x85\x8a\xad<8>\xd1\xff\n\x0f\x9b\xdf#\x1dT\xa0\f\x9e\x88100644 CHANGELOG\x00\xd3\xffS\xe0VJ\x9f\x87\xd8\xe8Kn(\xe5\x06\x0eQp\b\xaa100644 LICENSE\x00\xc1\x92\xbdj$\xea\x1a\xb0\x1dxhnA|\x8b\xdc|=\x19\u007f100644 binary.jpg\x00\xd5\xc0\xf4\xab\x81\x18\x97\xca\xdf\x03\xae\xc3X\xae`\xd2\x1f\x91\xc5\r40000 go\x00\xa3\x97q\xa7e\x1f\x97\xfa\xf5\xc7.\b\"M\x85\u007f\xc3Q3\xdb40000 json\x00Z\x87~j\x90j'C\xadnEٜ\x17\x93d*\xaf\x8e\xda40000 php\x00Xj\xf5gл^w\x1eI\xbd\xd9CO^\x0f\xb7m%\xfa"),
		}, {
			fixID:  "ref-deltas",
			offset: 85070,
			deps: map[int64]core.Object{
				84922: newObject(core.TreeObject, []byte("100644 .gitignore\x002\x85\x8a\xad<8>\xd1\xff\n\x0f\x9b\xdf#\x1dT\xa0\f\x9e\x88100644 CHANGELOG\x00\xd3\xffS\xe0VJ\x9f\x87\xd8\xe8Kn(\xe5\x06\x0eQp\b\xaa100644 LICENSE\x00\xc1\x92\xbdj$\xea\x1a\xb0\x1dxhnA|\x8b\xdc|=\x19\u007f100644 binary.jpg\x00\xd5\xc0\xf4\xab\x81\x18\x97\xca\xdf\x03\xae\xc3X\xae`\xd2\x1f\x91\xc5\r")),
				84849: newObject(core.TreeObject, []byte("100644 .gitignore\x002\x85\x8a\xad<8>\xd1\xff\n\x0f\x9b\xdf#\x1dT\xa0\f\x9e\x88100644 CHANGELOG\x00\xd3\xffS\xe0VJ\x9f\x87\xd8\xe8Kn(\xe5\x06\x0eQp\b\xaa100644 LICENSE\x00\xc1\x92\xbdj$\xea\x1a\xb0\x1dxhnA|\x8b\xdc|=\x19\u007f100644 binary.jpg\x00\xd5\xc0\xf4\xab\x81\x18\x97\xca\xdf\x03\xae\xc3X\xae`\xd2\x1f\x91\xc5\r40000 go\x00\xa3\x97q\xa7e\x1f\x97\xfa\xf5\xc7.\b\"M\x85\u007f\xc3Q3\xdb40000 json\x00Z\x87~j\x90j'C\xadnEٜ\x17\x93d*\xaf\x8e\xda40000 php\x00Xj\xf5gл^w\x1eI\xbd\xd9CO^\x0f\xb7m%\xfa")),
				83607: newObject(core.TreeObject, []byte("100644 .gitignore\x002\x85\x8a\xad<8>\xd1\xff\n\x0f\x9b\xdf#\x1dT\xa0\f\x9e\x88100644 CHANGELOG\x00\xd3\xffS\xe0VJ\x9f\x87\xd8\xe8Kn(\xe5\x06\x0eQp\b\xaa100644 LICENSE\x00\xc1\x92\xbdj$\xea\x1a\xb0\x1dxhnA|\x8b\xdc|=\x19\u007f100644 binary.jpg\x00\xd5\xc0\xf4\xab\x81\x18\x97\xca\xdf\x03\xae\xc3X\xae`\xd2\x1f\x91\xc5\r40000 go\x00\xa3\x97q\xa7e\x1f\x97\xfa\xf5\xc7.\b\"M\x85\u007f\xc3Q3\xdb40000 json\x00Z\x87~j\x90j'C\xadnEٜ\x17\x93d*\xaf\x8e\xda40000 php\x00Xj\xf5gл^w\x1eI\xbd\xd9CO^\x0f\xb7m%\xfa40000 vendor\x00\xcfJ\xa3\xb3\x89t\xfb}\x81\xf3g\xc0\x83\x0f}x\xd6Z\xb8k")),
			},
			expHash:    core.NewHash("eba74343e2f15d62adedfd8c883ee0262b5c8021"),
			expType:    core.TreeObject,
			expContent: []byte("100644 .gitignore\x002\x85\x8a\xad<8>\xd1\xff\n\x0f\x9b\xdf#\x1dT\xa0\f\x9e\x88100644 LICENSE\x00\xc1\x92\xbdj$\xea\x1a\xb0\x1dxhnA|\x8b\xdc|=\x19\u007f100644 binary.jpg\x00\xd5\xc0\xf4\xab\x81\x18\x97\xca\xdf\x03\xae\xc3X\xae`\xd2\x1f\x91\xc5\r"),
		},
	} {
		com := Commentf("test %d) fixture id = %s", i, test.fixID)
		fix, ok := s.fixtures[test.fixID]
		c.Assert(ok, Equals, true, com)

		err := fix.seek(test.offset)
		c.Assert(err, IsNil, com)
		p := fix.parser
		for k, v := range test.deps {
			err = p.Remember(k, v)
			c.Assert(err, IsNil, com)
		}

		_, _, err = p.ReadObjectTypeAndLength()
		c.Assert(err, IsNil, com)

		beforeHash, err := p.Offset()
		c.Assert(err, IsNil, com)

		hash, err := p.ReadHash()
		c.Assert(err, IsNil, com)
		c.Assert(hash, Equals, test.expHash, com)

		err = fix.seek(beforeHash)
		c.Assert(err, IsNil, com)

		cont, typ, err := p.ReadREFDeltaObjectContent()
		c.Assert(err, IsNil, com)
		c.Assert(typ, Equals, test.expType, com)
		c.Assert(cont, DeepEquals, test.expContent, com)

		p.ForgetAll()
	}
}

func newObject(t core.ObjectType, c []byte) *memory.Object {
	return memory.NewObject(t, int64(len(c)), c)
}

func (s *ParserSuite) TestReadHeaderBadSignatureError(c *C) {
	data := []byte{
		0x50, 0x42, 0x43, 0x4b, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x00, 0x00, 0x50,
	}
	p := NewParser(NewSeekable(bytes.NewReader(data)))

	_, err := p.ReadHeader()
	c.Assert(err, ErrorMatches, ErrBadSignature.Error())
}

func (s *ParserSuite) TestReadHeaderEmptyPackfileError(c *C) {
	data := []byte{}
	p := NewParser(NewSeekable(bytes.NewReader(data)))

	_, err := p.ReadHeader()
	c.Assert(err, ErrorMatches, ErrEmptyPackfile.Error())
}

func (s *ParserSuite) TestReadHeaderUnsupportedVersionError(c *C) {
	data := []byte{
		0x50, 0x41, 0x43, 0x4b, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x50,
	}
	p := NewParser(NewSeekable(bytes.NewReader(data)))

	_, err := p.ReadHeader()
	c.Assert(err, ErrorMatches, ErrUnsupportedVersion.Error()+".*")
}

func (s *ParserSuite) TestReadHeader(c *C) {
	data := []byte{
		0x50, 0x41, 0x43, 0x4b, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x00, 0x00, 0x50,
	}
	p := NewParser(NewSeekable(bytes.NewReader(data)))

	count, err := p.ReadHeader()
	c.Assert(err, IsNil)
	c.Assert(count, Equals, uint32(0x50))
}
