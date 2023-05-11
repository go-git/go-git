package object

import (
	"fmt"
	"io"
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

func (s *TagSuite) TestSSHSignatureSerialization(c *C) {
	encoded := &plumbing.MemoryObject{}
	decoded := &Tag{}
	tag := s.tag(c, plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))

	signature := `-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
0D7wOhjWVbYZH6KugAAAADZ2l0AAAAAAAAAAZzaGE1MTIAAABTAAAAC3NzaC1lZDI1NTE5
AAAAQIYHMhSVV9L2xwJuV8eWMLjThya8yXgCHDzw3p01D19KirrabW0veiichPB5m+Ihtr
MKEQruIQWJb+8HVXwssA4=
-----END SSH SIGNATURE-----`
	tag.PGPSignature = signature

	err := tag.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
	c.Assert(decoded.PGPSignature, Equals, signature)
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
	objectText := `object f6685df0aac4b5adf9eeb760e6d447145c5d0b56
type commit
tag v1.5
tagger Máximo Cuadros <mcuadros@gmail.com> 1618566233 +0200

signed tag
-----BEGIN PGP SIGNATURE-----

iQGzBAABCAAdFiEE/h5sbbqJFh9j1AdUSqtFFGopTmwFAmB5XFkACgkQSqtFFGop
TmxvgAv+IPjX5WCLFUIMx8hquMZp1VkhQrseE7rljUYaYpga8gZ9s4kseTGhy7Un
61U3Ro6cTPEiQF/FkAGzSdPuGqv0ARBqHDX2tUI9+Zs/K8aG8tN+JTaof0gBcTyI
BLbZVYDTxbS9whxSDewQd0OvBG1m9ISLUhjXo6mbaVvrKXNXTHg40MPZ8ZxjR/vN
hxXXoUVnFyEDo+v6nK56mYtapThDaQQHHzD6D3VaCq3Msog7qAh9/ZNBmgb88aQ3
FoK8PHMyr5elsV3mE9bciZBUc+dtzjOvp94uQ5ZKUXaPusXaYXnKpVnzhyer6RBI
gJLWtPwAinqmN41rGJ8jDAGrpPNjaRrMhGtbyVUPUf19OxuUIroe77sIIKTP0X2o
Wgp56dYpTst0JcGv/FYCeau/4pTRDfwHAOcDiBQ/0ag9IrZp9P8P9zlKmzNPEraV
pAe1/EFuhv2UDLucAiWM8iDZIcw8iN0OYMOGUmnk0WuGIo7dzLeqMGY+ND5n5Z8J
sZC//k6m
=VhHy
-----END PGP SIGNATURE-----
`

	armoredKeyRing := `
-----BEGIN PGP PUBLIC KEY BLOCK-----

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
=oYTT
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
	payload, err := io.ReadAll(er)
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
