package packfile

import (
	"io"
	"testing"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/fixtures"
	"gopkg.in/src-d/go-git.v4/formats/idxfile"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ReaderSuite struct{}

var _ = Suite(&ReaderSuite{})

func (s *ReaderSuite) SetUpSuite(c *C) {
	fixtures.RootFolder = "../../fixtures"
}

func (s *ReaderSuite) TestDecode(c *C) {
	fixtures.Basic().Test(c, func(f *fixtures.Fixture) {
		scanner := NewScanner(f.Packfile())
		storage := memory.NewStorage()

		d := NewDecoder(scanner, storage.ObjectStorage())

		ch, err := d.Decode()
		c.Assert(err, IsNil)
		c.Assert(ch, Equals, f.PackfileHash)

		AssertObjects(c, storage, []string{
			"918c48b83bd081e863dbe1b80f8998f058cd8294",
			"af2d6a6954d532f8ffb47615169c8fdf9d383a1a",
			"1669dce138d9b841a518c64b10914d88f5e488ea",
			"a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
			"b8e471f58bcbca63b07bda20e428190409c2db47",
			"35e85108805c84807bc66a02d91535e1e24b38b9",
			"b029517f6300c2da0f4b651b8642506cd6aaf45d",
			"32858aad3c383ed1ff0a0f9bdf231d54a00c9e88",
			"d3ff53e0564a9f87d8e84b6e28e5060e517008aa",
			"c192bd6a24ea1ab01d78686e417c8bdc7c3d197f",
			"d5c0f4ab811897cadf03aec358ae60d21f91c50d",
			"49c6bb89b17060d7b4deacb7b338fcc6ea2352a9",
			"cf4aa3b38974fb7d81f367c0830f7d78d65ab86b",
			"9dea2395f5403188298c1dabe8bdafe562c491e3",
			"586af567d0bb5e771e49bdd9434f5e0fb76d25fa",
			"9a48f23120e880dfbe41f7c9b7b708e9ee62a492",
			"5a877e6a906a2743ad6e45d99c1793642aaf8eda",
			"c8f1d8c61f9da76f4cb49fd86322b6e685dba956",
			"a8d315b2b1c615d43042c3a62402b8a54288cf5c",
			"a39771a7651f97faf5c72e08224d857fc35133db",
			"880cd14280f4b9b6ed3986d6671f907d7cc2a198",
			"fb72698cab7617ac416264415f13224dfd7a165e",
			"4d081c50e250fa32ea8b1313cf8bb7c2ad7627fd",
			"eba74343e2f15d62adedfd8c883ee0262b5c8021",
			"c2d30fa8ef288618f65f6eed6e168e0d514886f4",
			"8dcef98b1d52143e1e2dbc458ffe38f925786bf2",
			"aa9b383c260e1d05fbbf6b30a02914555e20c725",
			"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
			"dbd3641b371024f44d0e469a9c8f5457b0660de1",
			"e8d3ffab552895c19b9fcf7aa264d277cde33881",
			"7e59600739c96546163833214c36459e324bad0a",
		})

	})
}
func (s *ReaderSuite) TestDecodeCRCs(c *C) {
	f := fixtures.Basic().ByTag("ofs-delta").One()

	scanner := NewScanner(f.Packfile())
	storage := memory.NewStorage()

	d := NewDecoder(scanner, storage.ObjectStorage())
	_, err := d.Decode()
	c.Assert(err, IsNil)

	var sum uint64
	for _, crc := range d.CRCs() {
		sum += uint64(crc)
	}

	c.Assert(int(sum), Equals, 78022211966)
}

func (s *ReaderSuite) TestReadObjectAt(c *C) {
	fixtures.Basic().Test(c, func(f *fixtures.Fixture) {
		scanner := NewScanner(f.Packfile())
		storage := memory.NewStorage()

		d := NewDecoder(scanner, storage.ObjectStorage())

		// when the packfile is ref-delta based, the offsets are required
		if f.Is("ref-delta") {
			offsets := getOffsetsFromIdx(f.Idx())
			d.SetOffsets(offsets)
		}

		// the objects at reference 186, is a delta, so should be recall,
		// without being read before.
		obj, err := d.ReadObjectAt(186)
		c.Assert(err, IsNil)
		c.Assert(obj.Hash().String(), Equals, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	})
}

func AssertObjects(c *C, s *memory.Storage, expects []string) {
	o := s.ObjectStorage().(*memory.ObjectStorage)

	c.Assert(len(expects), Equals, len(o.Objects))
	for _, exp := range expects {
		obt, err := o.Get(core.AnyObject, core.NewHash(exp))
		c.Assert(err, IsNil)
		c.Assert(obt.Hash().String(), Equals, exp)
	}
}

func getOffsetsFromIdx(r io.Reader) map[core.Hash]int64 {
	idx := &idxfile.Idxfile{}
	err := idxfile.NewDecoder(r).Decode(idx)
	if err != nil {
		panic(err)
	}

	offsets := make(map[core.Hash]int64)
	for _, e := range idx.Entries {
		offsets[e.Hash] = int64(e.Offset)
	}

	return offsets
}
