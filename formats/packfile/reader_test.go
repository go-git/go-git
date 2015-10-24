package packfile

import (
	"bytes"
	"encoding/base64"
	"os"

	. "gopkg.in/check.v1"
)

type ReaderSuite struct{}

var _ = Suite(&ReaderSuite{})

var packFileWithEmptyObjects = "UEFDSwAAAAIAAAALnw54nKXMQWoDMQxA0b1PoX2hSLIm44FSAlmXnEG2NYlhXAfHgdLb5Cy9WAM5Qpb/Lf7oZqArUpakyYtQjCoxZ5lmWXwwyuzJbHqAuYt2+x6QoyCyhYCKIa67lGameSLWvPh5JU0hsCg7vY1z6/D1d/8ptcHhprm3Kxz7KL/wUdOz96eqZXtPrX4CCeOOPU8Eb0iI7qG1jGGvXdxaNoPs/gHeNkp8lA94nKXMQUpDMRCA4X1OMXtBZpI3L3kiRXAtPcMkmWjgxZSYQultPEsv1oJHcPl/i38OVRC0IXF0lshrJorZEcpKmTEJYbA+B3aFzEmGfk9gpqJEsmnZNutXF71i1IURU/G0bsWWwJ6NnOdXH/Bx+73U1uH9LHn0HziOWa/w2tJfv302qftz6u0AtFh0wQdmeEJCNA9tdU7938WUuivEF5CczR11ZEsNnw54nKWMUQoCIRRF/13F+w/ijY6jQkTQd7SGpz5LyAxzINpNa2ljTbSEPu/hnNsbM4TJTzqyt561GdUUmJKT6K2MeiCVgnZWoY/iRo2vHVS0URrUS+e+dkqIEp11HMhh9IaUkRM6QXM/1waH9+uRS4X9TLHVOxxbz0/YlPDbu1OhfFmHWrYwjBKVNVaNsMIBUSy05N75vxeR8oXBiw8GoErCnwt4nKXMzQkCMRBA4XuqmLsgM2M2ZkAWwbNYQ341sCEQsyB2Yy02pmAJHt93eKOnBFpMNJqtl5CFxVIMomViomQSEWP2JrN3yq3j1jqc369HqQ1Oq4u93eHSR3nCoYZfH6/VlWUbWp2BNOPO7i1OsEFCVF+tZYz030XlsiRw6gPZ0jxaqwV4nDM0MDAzMVFIZHg299HsTRevOXt3a64rj7px6ElP8ERDiGQSQ2uoXe8RrcodS5on+J4/u8HjD4NDKFQyRS8tPx+rbgDt3yiEMHicAwAAAAABPnicS0wEAa4kMOACACTjBKdkZXici7aaYAUAA3gBYKoDeJwzNDAwMzFRSGR4NvfR7E0Xrzl7d2uuK4+6cehJT/BEQ4hkEsOELYFJvS2eX47UJdVttFQrenrmzQwA13MaiDd4nEtMBAEuAApMAlGtAXicMzQwMDMxUUhkeDb30exNF685e3drriuPunHoSU/wRACvkA258N/i8hVXx9CiAZzvFXNIhCuSFmE="

func (s *ReaderSuite) TestReadPackfile(c *C) {
	data, _ := base64.StdEncoding.DecodeString(packFileWithEmptyObjects)
	d := bytes.NewReader(data)

	r, err := NewPackfileReader(d, nil)
	c.Assert(err, IsNil)

	p, err := r.Read()
	c.Assert(err, IsNil)

	c.Assert(p.ObjectCount, Equals, 11)
	c.Assert(p.Commits, HasLen, 4)
	c.Assert(p.Commits[NewHash("db4002e880a08bf6cc7217512ad937f1ac8824a2")], NotNil)
	c.Assert(p.Commits[NewHash("551fe11a9ef992763b7e0be4500cf7169f2f8575")], NotNil)
	c.Assert(p.Commits[NewHash("3d8d2705c6b936ceff0020989eca90db7a372609")], NotNil)
	c.Assert(p.Commits[NewHash("778c85ff95b5514fea0ba4c7b6a029d32e2c3b96")], NotNil)

	c.Assert(p.Trees, HasLen, 4)
	c.Assert(p.Trees[NewHash("af01d4cac3441bba4bdd4574938e1d231ee5d45e")], NotNil)
	c.Assert(p.Trees[NewHash("a028c5b32117ed11bd310a61d50ca10827d853f1")], NotNil)
	c.Assert(p.Trees[NewHash("c6b65deb8be57436ceaf920b82d51a3fc59830bd")], NotNil)
	c.Assert(p.Trees[NewHash("496d6428b9cf92981dc9495211e6e1120fb6f2ba")], NotNil)

	c.Assert(p.Blobs, HasLen, 3)
	c.Assert(p.Blobs[NewHash("85553e8dc42a79b8a483904dcfcdb048fc004055")], NotNil)
	c.Assert(p.Blobs[NewHash("90b451628d8449f4c47e627eb1392672e5ccec98")], NotNil)
	c.Assert(p.Blobs[NewHash("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")], NotNil)
}

func (s *ReaderSuite) TestReadPackfileInvalid(c *C) {
	r, err := NewPackfileReader(bytes.NewReader([]byte("dasdsadasas")), nil)
	c.Assert(err, IsNil)

	_, err = r.Read()
	_, ok := err.(*ReaderError)
	c.Assert(ok, Equals, true)
}

func (s *ReaderSuite) TestReadPackfileRefDelta(c *C) {
	d, err := os.Open("fixtures/git-fixture.ref-delta")
	c.Assert(err, IsNil)

	r, err := NewPackfileReader(d, nil)
	c.Assert(err, IsNil)

	p, err := r.Read()
	c.Assert(err, IsNil)

	s.AssertGitFixture(c, p)
}

func (s *ReaderSuite) TestReadPackfileOfsDelta(c *C) {
	d, err := os.Open("fixtures/git-fixture.ofs-delta")
	c.Assert(err, IsNil)

	r, err := NewPackfileReader(d, nil)
	c.Assert(err, IsNil)

	p, err := r.Read()
	c.Assert(err, IsNil)

	s.AssertGitFixture(c, p)
}

func (s *ReaderSuite) AssertGitFixture(c *C, p *Packfile) {

	c.Assert(p.ObjectCount, Equals, 28)

	c.Assert(p.Commits, HasLen, 8)
	c.Assert(p.Trees, HasLen, 11)
	c.Assert(p.Blobs, HasLen, 9)

	commits := []string{
		"a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
		"b8e471f58bcbca63b07bda20e428190409c2db47",
		"35e85108805c84807bc66a02d91535e1e24b38b9",
		"b029517f6300c2da0f4b651b8642506cd6aaf45d",
		"6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
		"918c48b83bd081e863dbe1b80f8998f058cd8294",
		"af2d6a6954d532f8ffb47615169c8fdf9d383a1a",
		"1669dce138d9b841a518c64b10914d88f5e488ea",
	}

	for _, hash := range commits {
		c.Assert(p.Commits[NewHash(hash)], NotNil)
	}

	trees := []string{
		"aa9b383c260e1d05fbbf6b30a02914555e20c725",
		"cf4aa3b38974fb7d81f367c0830f7d78d65ab86b",
		"586af567d0bb5e771e49bdd9434f5e0fb76d25fa",
		"4d081c50e250fa32ea8b1313cf8bb7c2ad7627fd",
		"eba74343e2f15d62adedfd8c883ee0262b5c8021",
		"c2d30fa8ef288618f65f6eed6e168e0d514886f4",
		"8dcef98b1d52143e1e2dbc458ffe38f925786bf2",
		"5a877e6a906a2743ad6e45d99c1793642aaf8eda",
		"a8d315b2b1c615d43042c3a62402b8a54288cf5c",
		"a39771a7651f97faf5c72e08224d857fc35133db",
		"fb72698cab7617ac416264415f13224dfd7a165e",
	}

	for _, hash := range trees {
		c.Assert(p.Trees[NewHash(hash)], NotNil)
	}

	blobs := []string{
		"d5c0f4ab811897cadf03aec358ae60d21f91c50d",
		"9a48f23120e880dfbe41f7c9b7b708e9ee62a492",
		"c8f1d8c61f9da76f4cb49fd86322b6e685dba956",
		"880cd14280f4b9b6ed3986d6671f907d7cc2a198",
		"32858aad3c383ed1ff0a0f9bdf231d54a00c9e88",
		"d3ff53e0564a9f87d8e84b6e28e5060e517008aa",
		"c192bd6a24ea1ab01d78686e417c8bdc7c3d197f",
		"49c6bb89b17060d7b4deacb7b338fcc6ea2352a9",
		"9dea2395f5403188298c1dabe8bdafe562c491e3",
	}

	for _, hash := range blobs {
		c.Assert(p.Blobs[NewHash(hash)], NotNil)
	}
}
