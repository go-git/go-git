package object

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type TagSuite struct {
	suite.Suite
	BaseObjectsSuite
}

func TestTagSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(TagSuite))
}

func (s *TagSuite) SetupSuite() {
	s.BaseObjectsSuite.SetupSuite(s.T())
	storer := filesystem.NewStorage(fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit(), cache.NewObjectLRUDefault())
	s.Storer = storer
}

func (s *TagSuite) TestNameIDAndType() {
	h := plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69")
	tag := s.tag(h)
	s.Equal("annotated-tag", tag.Name)
	s.Equal(tag.ID(), h)
	s.Equal(tag.Type(), plumbing.TagObject)
}

func (s *TagSuite) TestTagger() {
	tag := s.tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	s.Equal("Máximo Cuadros <mcuadros@gmail.com>", tag.Tagger.String())
}

func (s *TagSuite) TestAnnotated() {
	tag := s.tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	s.Equal("example annotated tag\n", tag.Message)

	commit, err := tag.Commit()
	s.NoError(err)
	s.Equal(plumbing.CommitObject, commit.Type())
	s.Equal("f7b877701fbf855b44c0a9e86f3fdce2c298b07f", commit.ID().String())
}

func (s *TagSuite) TestCommitError() {
	tag := s.tag(plumbing.NewHash("fe6cb94756faa81e5ed9240f9191b833db5f40ae"))

	commit, err := tag.Commit()
	s.Nil(commit)
	s.NotNil(err)
	s.ErrorIs(err, ErrUnsupportedObject)
}

func (s *TagSuite) TestCommit() {
	tag := s.tag(plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))
	s.Equal("a tagged commit\n", tag.Message)

	commit, err := tag.Commit()
	s.NoError(err)
	s.Equal(plumbing.CommitObject, commit.Type())
	s.Equal("f7b877701fbf855b44c0a9e86f3fdce2c298b07f", commit.ID().String())
}

func (s *TagSuite) TestBlobError() {
	tag := s.tag(plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))

	commit, err := tag.Blob()
	s.Nil(commit)
	s.NotNil(err)
	s.ErrorIs(err, ErrUnsupportedObject)
}

func (s *TagSuite) TestBlob() {
	tag := s.tag(plumbing.NewHash("fe6cb94756faa81e5ed9240f9191b833db5f40ae"))
	s.Equal("a tagged blob\n", tag.Message)

	blob, err := tag.Blob()
	s.NoError(err)
	s.Equal(plumbing.BlobObject, blob.Type())
	s.Equal("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", blob.ID().String())
}

func (s *TagSuite) TestTreeError() {
	tag := s.tag(plumbing.NewHash("fe6cb94756faa81e5ed9240f9191b833db5f40ae"))

	tree, err := tag.Tree()
	s.Nil(tree)
	s.NotNil(err)
	s.ErrorIs(err, ErrUnsupportedObject)
}

func (s *TagSuite) TestTree() {
	tag := s.tag(plumbing.NewHash("152175bf7e5580299fa1f0ba41ef6474cc043b70"))
	s.Equal("a tagged tree\n", tag.Message)

	tree, err := tag.Tree()
	s.NoError(err)
	s.Equal(plumbing.TreeObject, tree.Type())
	s.Equal("70846e9a10ef7b41064b40f07713d5b8b9a8fc73", tree.ID().String())
}

func (s *TagSuite) TestTreeFromCommit() {
	tag := s.tag(plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))
	s.Equal("a tagged commit\n", tag.Message)

	tree, err := tag.Tree()
	s.NoError(err)
	s.Equal(plumbing.TreeObject, tree.Type())
	s.Equal("70846e9a10ef7b41064b40f07713d5b8b9a8fc73", tree.ID().String())
}

func (s *TagSuite) TestObject() {
	tag := s.tag(plumbing.NewHash("ad7897c0fb8e7d9a9ba41fa66072cf06095a6cfc"))

	obj, err := tag.Object()
	s.NoError(err)
	s.Equal(plumbing.CommitObject, obj.Type())
	s.Equal("f7b877701fbf855b44c0a9e86f3fdce2c298b07f", obj.ID().String())
}

func (s *TagSuite) TestTagItter() {
	iter, err := s.Storer.IterEncodedObjects(plumbing.TagObject)
	s.NoError(err)

	var count int
	i := NewTagIter(s.Storer, iter)
	tag, err := i.Next()
	s.NoError(err)
	s.NotNil(tag)
	s.Equal(plumbing.TagObject, tag.Type())

	err = i.ForEach(func(t *Tag) error {
		s.NotNil(t)
		s.Equal(plumbing.TagObject, t.Type())
		count++

		return nil
	})

	s.NoError(err)
	s.Equal(3, count)

	tag, err = i.Next()
	s.ErrorIs(err, io.EOF)
	s.Nil(tag)
}

func (s *TagSuite) TestTagIterError() {
	iter, err := s.Storer.IterEncodedObjects(plumbing.TagObject)
	s.NoError(err)

	randomErr := fmt.Errorf("a random error")
	i := NewTagIter(s.Storer, iter)
	err = i.ForEach(func(_ *Tag) error {
		return randomErr
	})

	s.NotNil(err)
	s.ErrorIs(err, randomErr)
}

func (s *TagSuite) TestTagDecodeWrongType() {
	newTag := &Tag{}
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.BlobObject)
	err := newTag.Decode(obj)
	s.ErrorIs(err, ErrUnsupportedObject)
}

func (s *TagSuite) TestTagEncodeDecodeIdempotent() {
	ts, err := time.ParseInLocation(time.RFC3339, "2006-01-02T15:04:05-07:00", time.UTC)
	s.NoError(err)
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
		err := tag.Encode(obj)
		s.NoError(err)
		newTag := &Tag{}
		err = newTag.Decode(obj)
		s.NoError(err)
		tag.Hash = obj.Hash()
		s.Equal(tag, newTag)
	}
}

func (s *TagSuite) TestString() {
	tag := s.tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	s.Equal(""+
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
		tag.String(),
	)

	tag = s.tag(plumbing.NewHash("152175bf7e5580299fa1f0ba41ef6474cc043b70"))
	s.Equal(""+
		"tag tree-tag\n"+
		"Tagger: Máximo Cuadros <mcuadros@gmail.com>\n"+
		"Date:   Wed Sep 21 21:17:56 2016 +0200\n"+
		"\n"+
		"a tagged tree\n"+
		"\n",
		tag.String(),
	)
}

func (s *TagSuite) TestStringNonCommit() {
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
	s.NoError(err)

	s.Equal(
		"tag TAG TWO\n"+
			"Tagger:  <>\n"+
			"Date:   Thu Jan 01 00:00:00 1970 +0000\n"+
			"\n"+
			"tag two\n",
		tag.String(),
	)
}

func (s *TagSuite) TestLongTagNameSerialization() {
	encoded := &plumbing.MemoryObject{}
	decoded := &Tag{}
	tag := s.tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))

	longName := "my tag: name " + strings.Repeat("test", 4096) + " OK"
	tag.Name = longName

	err := tag.Encode(encoded)
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
	s.Equal(longName, decoded.Name)
}

func (s *TagSuite) TestPGPSignatureSerialization() {
	encoded := &plumbing.MemoryObject{}
	decoded := &Tag{}
	tag := s.tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))

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
	tag.Signature = pgpsignature

	err := tag.Encode(encoded)
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
	s.Equal(pgpsignature, decoded.Signature)
}

func (s *TagSuite) TestSSHSignatureSerialization() {
	encoded := &plumbing.MemoryObject{}
	decoded := &Tag{}
	tag := s.tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))

	signature := `-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgij/EfHS8tCjolj5uEANXgKzFfp
0D7wOhjWVbYZH6KugAAAADZ2l0AAAAAAAAAAZzaGE1MTIAAABTAAAAC3NzaC1lZDI1NTE5
AAAAQIYHMhSVV9L2xwJuV8eWMLjThya8yXgCHDzw3p01D19KirrabW0veiichPB5m+Ihtr
MKEQruIQWJb+8HVXwssA4=
-----END SSH SIGNATURE-----`
	tag.Signature = signature

	err := tag.Encode(encoded)
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
	s.Equal(signature, decoded.Signature)
}

func (s *TagSuite) TestVerify() {
	ts := time.Unix(1617403017, 0)
	loc, _ := time.LoadLocation("UTC")
	tag := &Tag{
		Name:   "v0.2",
		Tagger: Signature{Name: "go-git", Email: "go-git@example.com", When: ts.In(loc)},
		Message: `This is a signed tag
`,
		TargetType: plumbing.CommitObject,
		Target:     plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Signature: `
-----BEGIN PGP SIGNATURE-----

iHUEABYKAB0WIQTMqU0ycQ3f6g3PMoWMmmmF4LuV8QUCYGeciQAKCRCMmmmF4LuV
8ZoDAP4j9msumYymfHgS3y7jpxPcSyiOMlXjipr2upspvXJ6ewD+K+OPC4pGW7Aq
8UDK8r6qhaloxATcV/LUrvAW2yz4PwM=
=PD+s
-----END PGP SIGNATURE-----
`,
	}

	// Test with no verifier (nil) returns ErrNilVerifier.
	_, err := tag.Verify()
	s.ErrorIs(err, ErrNilVerifier)

	// Test VerifyWith with a mock verifier.
	mock := &mockVerifier{result: &VerificationResult{
		Type:  SignatureTypeOpenPGP,
		Valid: true,
		KeyID: "test-key",
	}}
	result, err := tag.VerifyWith(mock)
	s.NoError(err)
	s.True(result.Valid)
	s.Equal("test-key", result.KeyID)

	// Verify the mock received the signature bytes.
	s.NotEmpty(mock.gotSignature)
	s.NotEmpty(mock.gotMessage)

	// Test unsigned tag returns ErrNoSignature.
	unsigned := &Tag{Name: "unsigned", Message: "unsigned tag\n"}
	_, err = unsigned.VerifyWith(mock)
	s.ErrorIs(err, ErrNoSignature)
}

func (s *TagSuite) TestDecodeAndVerify() {
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

	tagEncodedObject := &plumbing.MemoryObject{}

	_, err := tagEncodedObject.Write([]byte(objectText))
	tagEncodedObject.SetType(plumbing.TagObject)
	s.NoError(err)

	tag := &Tag{}
	err = tag.Decode(tagEncodedObject)
	s.NoError(err)

	mock := &mockVerifier{result: &VerificationResult{
		Type:  SignatureTypeOpenPGP,
		Valid: true,
		KeyID: "test-key",
	}}
	_, err = tag.VerifyWith(mock)
	s.NoError(err)
}

func (s *TagSuite) TestEncodeWithoutSignature() {
	// Similar to TestString since no signature
	encoded := &plumbing.MemoryObject{}
	tag := s.tag(plumbing.NewHash("b742a2a9fa0afcfa9a6fad080980fbc26b007c69"))
	err := tag.EncodeWithoutSignature(encoded)
	s.NoError(err)
	er, err := encoded.Reader()
	s.NoError(err)
	payload, err := io.ReadAll(er)
	s.NoError(err)

	s.Equal(""+
		"object f7b877701fbf855b44c0a9e86f3fdce2c298b07f\n"+
		"type commit\n"+
		"tag annotated-tag\n"+
		"tagger Máximo Cuadros <mcuadros@gmail.com> 1474485215 +0200\n"+
		"\n"+
		"example annotated tag\n",
		string(payload),
	)
}
