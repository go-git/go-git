package object

import (
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"

	. "gopkg.in/check.v1"
)

type TagSuite struct {
	BaseObjectsSuite
}

var _ = Suite(&TagSuite{})

func (s *TagSuite) SetUpSuite(c *C) {
	s.BaseObjectsSuite.SetUpSuite(c)
	storer := filesystem.NewStorage(fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit(), cache.NewObjectLRUDefault())
	s.Storer = storer
}

func (s *TagSuite) TestNameIDAndType(c *C) {
	h := plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69")
	tag := s.tag(c, h)
	c.Assert(tag.Name, Equals, "annotated-tag")
	c.Assert(h, Equals, tag.ID())
	c.Assert(plumbing.TagObject, Equals, tag.Type())
}

func (s *TagSuite) TestTagger(c *C) {
	tag := s.tag(c, plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	c.Assert(tag.Tagger.String(), Equals, "Máximo Cuadros <mcuadros@gmail.com>")
}

func (s *TagSuite) TestAnnotated(c *C) {
	tag := s.tag(c, plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	c.Assert(tag.Message, Equals, "example annotated tag\n")

	commit, err := tag.Commit()
	c.Assert(err, IsNil)
	c.Assert(commit.Type(), Equals, plumbing.CommitObject)
	c.Assert(commit.ID().String(), Equals, "f7b877701fbf855b44c0a9e86f3fdce2c298b07f")
}

func (s *TagSuite) TestCommitError(c *C) {
	tag := s.tag(c, plumbing.NewHash("fe6cb94756faa81e5ed9240f9191b833db5f40ae"))

	commit, err := tag.Commit()
	c.Assert(commit, IsNil)
	c.Assert(err, NotNil)
	c.Assert(err, Equals, ErrUnsupportedObject)
}

func (s *TagSuite) TestCommit(c *C) {
	tag := s.tag(c, plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))
	c.Assert(tag.Message, Equals, "a tagged commit\n")

	commit, err := tag.Commit()
	c.Assert(err, IsNil)
	c.Assert(commit.Type(), Equals, plumbing.CommitObject)
	c.Assert(commit.ID().String(), Equals, "f7b877701fbf855b44c0a9e86f3fdce2c298b07f")
}

func (s *TagSuite) TestBlobError(c *C) {
	tag := s.tag(c, plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))

	commit, err := tag.Blob()
	c.Assert(commit, IsNil)
	c.Assert(err, NotNil)
	c.Assert(err, Equals, ErrUnsupportedObject)
}

func (s *TagSuite) TestBlob(c *C) {
	tag := s.tag(c, plumbing.NewHash("fe6cb94756faa81e5ed9240f9191b833db5f40ae"))
	c.Assert(tag.Message, Equals, "a tagged blob\n")

	blob, err := tag.Blob()
	c.Assert(err, IsNil)
	c.Assert(blob.Type(), Equals, plumbing.BlobObject)
	c.Assert(blob.ID().String(), Equals, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")
}

func (s *TagSuite) TestTreeError(c *C) {
	tag := s.tag(c, plumbing.NewHash("fe6cb94756faa81e5ed9240f9191b833db5f40ae"))

	tree, err := tag.Tree()
	c.Assert(tree, IsNil)
	c.Assert(err, NotNil)
	c.Assert(err, Equals, ErrUnsupportedObject)
}

func (s *TagSuite) TestTree(c *C) {
	tag := s.tag(c, plumbing.NewHash("152175bf7e5580299fa1f0ba41ef6474cc043b70"))
	c.Assert(tag.Message, Equals, "a tagged tree\n")

	tree, err := tag.Tree()
	c.Assert(err, IsNil)
	c.Assert(tree.Type(), Equals, plumbing.TreeObject)
	c.Assert(tree.ID().String(), Equals, "70846e9a10ef7b41064b40f07713d5b8b9a8fc73")
}

func (s *TagSuite) TestTreeFromCommit(c *C) {
	tag := s.tag(c, plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))
	c.Assert(tag.Message, Equals, "a tagged commit\n")

	tree, err := tag.Tree()
	c.Assert(err, IsNil)
	c.Assert(tree.Type(), Equals, plumbing.TreeObject)
	c.Assert(tree.ID().String(), Equals, "70846e9a10ef7b41064b40f07713d5b8b9a8fc73")
}

func (s *TagSuite) TestObject(c *C) {
	tag := s.tag(c, plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))

	obj, err := tag.Object()
	c.Assert(err, IsNil)
	c.Assert(obj.Type(), Equals, plumbing.CommitObject)
	c.Assert(obj.ID().String(), Equals, "f7b877701fbf855b44c0a9e86f3fdce2c298b07f")
}

func (s *TagSuite) TestTagItter(c *C) {
	iter, err := s.Storer.IterEncodedObjects(plumbing.TagObject)
	c.Assert(err, IsNil)

	var count int
	i := NewTagIter(s.Storer, iter)
	tag, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(tag, NotNil)
	c.Assert(tag.Type(), Equals, plumbing.TagObject)

	err = i.ForEach(func(t *Tag) error {
		c.Assert(t, NotNil)
		c.Assert(t.Type(), Equals, plumbing.TagObject)
		count++

		return nil
	})

	c.Assert(err, IsNil)
	c.Assert(count, Equals, 3)

	tag, err = i.Next()
	c.Assert(err, Equals, io.EOF)
	c.Assert(tag, IsNil)
}

func (s *TagSuite) TestTagIterError(c *C) {
	iter, err := s.Storer.IterEncodedObjects(plumbing.TagObject)
	c.Assert(err, IsNil)

	randomErr := fmt.Errorf("a random error")
	i := NewTagIter(s.Storer, iter)
	err = i.ForEach(func(t *Tag) error {
		return randomErr
	})

	c.Assert(err, NotNil)
	c.Assert(err, Equals, randomErr)
}

func (s *TagSuite) TestTagDecodeWrongType(c *C) {
	newTag := &Tag{}
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.BlobObject)
	err := newTag.Decode(obj)
	c.Assert(err, Equals, ErrUnsupportedObject)
}

func (s *TagSuite) TestTagEncodeDecodeIdempotent(c *C) {
	ts, err := time.Parse(time.RFC3339, "2006-01-02T15:04:05-07:00")
	c.Assert(err, IsNil)
	tags := []*Tag{
		{
			Name:       "foo",
			Tagger:     Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Message:    "Message\n\nFoo\nBar\nBaz\n\n",
			TargetType: plumbing.BlobObject,
			Target:     plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		},
		{
			Name:       "foo",
			Tagger:     Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			TargetType: plumbing.BlobObject,
			Target:     plumbing.NewHash("b029517f6300c2da0f4b651b8642506cd6aaf45d"),
		},
	}
	for _, tag := range tags {
		obj := &plumbing.MemoryObject{}
		err = tag.Encode(obj)
		c.Assert(err, IsNil)
		newTag := &Tag{}
		err = newTag.Decode(obj)
		c.Assert(err, IsNil)
		tag.Hash = obj.Hash()
		c.Assert(newTag, DeepEquals, tag)
	}
}

func (s *TagSuite) TestString(c *C) {
	tag := s.tag(c, plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	c.Assert(tag.String(), Equals, ""+
		"tag annotated-tag\n"+
		"Tagger: Máximo Cuadros <mcuadros@gmail.com>\n"+
		"Date:   Wed Sep 21 21:13:35 2016 +0200\n"+
		"\n"+
		"example annotated tag\n"+
		"\n"+
		"commit f7b877701fbf855b44c0a9e86f3fdce2c298b07f\n"+
		"Author: Máximo Cuadros <mcuadros@gmail.com>\n"+
		"Date:   Wed Sep 21 21:10:52 2016 +0200\n"+
		"\n"+
		"    initial\n"+
		"\n",
	)

	tag = s.tag(c, plumbing.NewHash("152175bf7e5580299fa1f0ba41ef6474cc043b70"))
	c.Assert(tag.String(), Equals, ""+
		"tag tree-tag\n"+
		"Tagger: Máximo Cuadros <mcuadros@gmail.com>\n"+
		"Date:   Wed Sep 21 21:17:56 2016 +0200\n"+
		"\n"+
		"a tagged tree\n"+
		"\n",
	)
}

func (s *TagSuite) TestStringNonCommit(c *C) {
	store := memory.NewStorage()

	target := &Tag{
		Target:     plumbing.NewHash("TAGONE"),
		Name:       "TAG ONE",
		Message:    "tag one",
		TargetType: plumbing.TagObject,
	}

	targetObj := &plumbing.MemoryObject{}
	target.Encode(targetObj)
	store.SetEncodedObject(targetObj)

	tag := &Tag{
		Target:     targetObj.Hash(),
		Name:       "TAG TWO",
		Message:    "tag two",
		TargetType: plumbing.TagObject,
	}

	tagObj := &plumbing.MemoryObject{}
	tag.Encode(tagObj)
	store.SetEncodedObject(tagObj)

	tag, err := GetTag(store, tagObj.Hash())
	c.Assert(err, IsNil)

	c.Assert(tag.String(), Equals,
		"tag TAG TWO\n"+
			"Tagger:  <>\n"+
			"Date:   Thu Jan 01 00:00:00 1970 +0000\n"+
			"\n"+
			"tag two\n")
}

func (s *TagSuite) TestLongTagNameSerialization(c *C) {
	encoded := &plumbing.MemoryObject{}
	decoded := &Tag{}
	tag := s.tag(c, plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))

	longName := "my tag: name " + strings.Repeat("test", 4096) + " OK"
	tag.Name = longName

	err := tag.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
	c.Assert(decoded.Name, Equals, longName)
}

func (s *TagSuite) TestPGPSignatureSerialization(c *C) {
	encoded := &plumbing.MemoryObject{}
	decoded := &Tag{}
	tag := s.tag(c, plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))

	pgpsignature := `-----BEGIN PGP SIGNATURE-----

iQEcBAABAgAGBQJTZbQlAAoJEF0+sviABDDrZbQH/09PfE51KPVPlanr6q1v4/Ut
LQxfojUWiLQdg2ESJItkcuweYg+kc3HCyFejeDIBw9dpXt00rY26p05qrpnG+85b
hM1/PswpPLuBSr+oCIDj5GMC2r2iEKsfv2fJbNW8iWAXVLoWZRF8B0MfqX/YTMbm
ecorc4iXzQu7tupRihslbNkfvfciMnSDeSvzCpWAHl7h8Wj6hhqePmLm9lAYqnKp
8S5B/1SSQuEAjRZgI4IexpZoeKGVDptPHxLLS38fozsyi0QyDyzEgJxcJQVMXxVi
RUysgqjcpT8+iQM1PblGfHR4XAhuOqN5Fx06PSaFZhqvWFezJ28/CLyX5q+oIVk=
=EFTF
-----END PGP SIGNATURE-----
`
	tag.PGPSignature = pgpsignature

	err := tag.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
	c.Assert(decoded.PGPSignature, Equals, pgpsignature)
}

func (s *TagSuite) TestVerify(c *C) {
	ts := time.Unix(1617403017, 0)
	loc, _ := time.LoadLocation("UTC")
	tag := &Tag{
		Name:   "v0.2",
		Tagger: Signature{Name: "go-git", Email: "go-git@example.com", When: ts.In(loc)},
		Message: `This is a signed tag
`,
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		PGPSignature: `
-----BEGIN PGP SIGNATURE-----

iHUEABYKAB0WIQTMqU0ycQ3f6g3PMoWMmmmF4LuV8QUCYGeciQAKCRCMmmmF4LuV
8ZoDAP4j9msumYymfHgS3y7jpxPcSyiOMlXjipr2upspvXJ6ewD+K+OPC4pGW7Aq
8UDK8r6qhaloxATcV/LUrvAW2yz4PwM=
=PD+s
-----END PGP SIGNATURE-----
`,
	}

	armoredKeyRing := `
-----BEGIN PGP PUBLIC KEY BLOCK-----

mDMEYGeSihYJKwYBBAHaRw8BAQdAIs9A3YD/EghhAOkHDkxlUkpqYrXUXebLfmmX
+pdEK6C0D2dvLWdpdCB0ZXN0IGtleYiPBBMWCgA3FiEEzKlNMnEN3+oNzzKFjJpp
heC7lfEFAmBnkooCGyMECwkIBwUVCgkICwUWAwIBAAIeAQIXgAAKCRCMmmmF4LuV
8a3jAQCi4hSqjj6J3ch290FvQaYPGwR+EMQTMBG54t+NN6sDfgD/aZy41+0dnFKl
qM/wLW5Wr9XvwH+1zXXbuSvfxasHowq4OARgZ5KKEgorBgEEAZdVAQUBAQdAXoQz
VTYug16SisAoSrxFnOmxmFu6efYgCAwXu0ZuvzsDAQgHiHgEGBYKACAWIQTMqU0y
cQ3f6g3PMoWMmmmF4LuV8QUCYGeSigIbDAAKCRCMmmmF4LuV8Q4QAQCKW5FnEdWW
lHYKeByw3JugnlZ0U3V/R20bCwDglst5UQEAtkN2iZkHtkPly9xapsfNqnrt2gTt
YIefGtzXfldDxg4=
=Psht
-----END PGP PUBLIC KEY BLOCK-----
`

	e, err := tag.Verify(armoredKeyRing)
	c.Assert(err, IsNil)

	_, ok := e.Identities["go-git test key"]
	c.Assert(ok, Equals, true)
}

func (s *TagSuite) TestDecodeAndVerify(c *C) {
	objectText := `object 7dba2f128d1298e385b28b56a7e1c579779eac82
type commit
tag v1.6
tagger Filip Navara <filip.navara@gmail.com> 1555269936 +0200

Hello

world

boo
-----BEGIN PGP SIGNATURE-----

iQEzBAABCAAdFiEEdRIEYXeoLk1t7PBDqeqoMkraaZ4FAlyziT4ACgkQqeqoMkra
aZ502wgAxG4+69l8PYfq45u1R3CCf4x0m5WwcYwvaa4ang0S9mExh/C32NHnpM/V
DbqMpAlFvBlixOsZ8FNWaM8VXnvRWyx64E6WnInxjx9+Wgv2fy5P1N5rtpvi+S2V
iGc0RQJlIloqXr7qPYDrwcbgg6AFg9EPhgJxLyizglu9nYvNsH1InaPXMjzgGX8+
3irnIYEMIrLcKPrCyHo4Q6gdBjEEBF8hFclPJ8OwXBPc6uNYjnDYx0me9TTQYqoG
oGgO/rADU9fy4c/Q1ZQpocba/ca6abRJ9LAx9VXFOSlQrMKLgHCYfqU/MAZXKcZM
6XXOL4+8Z3FJN6CapZKX7cdYB8LJnw==
=t5Px
-----END PGP SIGNATURE-----

`

	armoredKeyRing := `
-----BEGIN PGP PUBLIC KEY BLOCK-----

mQENBFyzedYBCADN3lVNUNkrjn0kfwKAxGQOI8a1977UaIq9ktFg+Uv4Jyq2Y59L
ZVx2WYk1iDaRhxhv203HV//CA/Hr4IoPjK53qAkg2bPyi8UuDbL+gU+4Z+IiSeXd
18ZcAbcYt188PWoUq9/82ofO8EiaBbUEEZJjEegLDtX8gxBDG0aI3Yj4Txj73mno
w6+E5HDkgPElmH3oNQcr8iK9U2Kuj+ZAHkzbWL++gDCPiLl2eWf0Cr1nlVsv6YLa
Fsn5vjMGT3dMJFc78ZqCHOeyYK7KHjW1EjzgqeG2eJVay+ZQ5zEx4Fp/dL0RdUSV
U7zslRiraaPxshdhYOjQ0o72RpSkP1G6+8OhABEBAAG0JUZpbGlwIE5hdmFyYSA8
ZmlsaXAubmF2YXJhQGdtYWlsLmNvbT6JAVQEEwEIAD4WIQR1EgRhd6guTW3s8EOp
6qgyStppngUCXLN51gIbAwUJA8JnAAULCQgHAgYVCgkICwIEFgIDAQIeAQIXgAAK
CRCp6qgyStppnlzjB/sFu7HqJrTRsnHsoWo2+nDeicXnR0VAhiLvv7uRRw4i90FJ
0zDwjAmIH+po6vPffWRMcWOFVvAwZCX7/XcvDNF9OupFj/aold334+VVN0ha47IQ
g44bJZie9mvLagEsqUXggpKQjd414Tk08aUucfaN9RFJIOGCwF05j2eXOBGR2HTe
FLq3obeObryEPf0c8N/nw4RQ8OOcq98gxiHx5Gk+nLCcJCTvOlc9ULqpJ2a6cZry
kxgSOI9dd74ilRQdpfPvoEeEGSqkY+daf+dhgSMT2mII0UJ6qQeY0DpCZZNsL8dr
PxR4SPRlzLBuJIpnHY21ebOqwOPOLjzR+J2RBufkuQENBFyzedYBCADTCglXrST6
DRz7Uq3zrrrzdCchHH0/+LgYOEoGs82UvdFfigQYGTydmXz27bHKfWNfGIa9IlLF
MhasFueCnKnmfVxnlINRdyAXv7Tmx4mSjuCEmGkvM1nPpdhxWXptnVMqhQMddiMO
N55bElDK2ftPc2s4dBmTItXXbet2kFZiv7MZBZpA4eRAHj5DDSwl8pnQArU50RDZ
q3qYKvAP/z2SLjekcOFtMhZ9BXMvwAW4FWV0ztpfP3LvUUb0T7fSo5cXlm/0eqwa
MUrUlbbwJMDg1/wJ3pbKhZlP+xXNLj5UE86TtfqNqaohOcIBdCsdTUQgbkLVlibP
JmZH7lGDhvi3ABEBAAGJATwEGAEIACYWIQR1EgRhd6guTW3s8EOp6qgyStppngUC
XLN51gIbDAUJA8JnAAAKCRCp6qgyStppntq1B/9bmw4XjEm5KyXwWnlAVGr8skXY
KIJr6drUOOwQzl7rxsJRjUsFdX0IjaZwx303G/23eQMIvVkoaWpHrT0Y7EsTQ55x
+GSuANhEzobks4spzQ66VW9FHRlRr5wg5PTwWnGtV/5QVSTY/zeC9R/AFUJFsDWe
tgHlNrb6MWx5EtypZDpAkubAMvD/QoZHX0oPXYAA2CugD4uSdzjf6Ys3xUuwjKKG
5hvimAg1/Hympq71Znb6Ec1m4ZM22Br7dcWHIX2GWfDPyRG+rYPu4Fk9KKAD4FRz
HdzbB2ak/HxIeCqmHVlmUqa+WfTMUJcsgOm3/ZFPCSoL6l0bz9Z1XVbiyD03
=+gC9
-----END PGP PUBLIC KEY BLOCK-----
`

	tagEncodedObject := &plumbing.MemoryObject{}

	_, err := tagEncodedObject.Write([]byte(objectText))
	tagEncodedObject.SetType(plumbing.TagObject)
	c.Assert(err, IsNil)

	tag := &Tag{}
	err = tag.Decode(tagEncodedObject)
	c.Assert(err, IsNil)

	_, err = tag.Verify(armoredKeyRing)
	c.Assert(err, IsNil)
}

func (s *TagSuite) TestEncodeWithoutSignature(c *C) {
	//Similar to TestString since no signature
	encoded := &plumbing.MemoryObject{}
	tag := s.tag(c, plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	err := tag.EncodeWithoutSignature(encoded)
	c.Assert(err, IsNil)
	er, err := encoded.Reader()
	c.Assert(err, IsNil)
	payload, err := ioutil.ReadAll(er)
	c.Assert(err, IsNil)

	c.Assert(string(payload), Equals, ""+
		"object f7b877701fbf855b44c0a9e86f3fdce2c298b07f\n"+
		"type commit\n"+
		"tag annotated-tag\n"+
		"tagger Máximo Cuadros <mcuadros@gmail.com> 1474485215 +0200\n"+
		"\n"+
		"example annotated tag\n",
	)
}
