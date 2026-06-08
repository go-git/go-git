package object

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v6"
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
	tagsDotgit, err := fixtures.ByURL("https://github.com/git-fixtures/tags.git").One().DotGit()
	s.Require().NoError(err)
	storer := filesystem.NewStorage(tagsDotgit, cache.NewObjectLRUDefault())
	s.T().Cleanup(func() {
		_ = storer.Close()
	})
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
		{
			Name:       "signed",
			Tagger:     Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Message:    "Signed tag\n",
			TargetType: plumbing.CommitObject,
			Target:     plumbing.NewHash("c029517f6300c2da0f4b651b8642506cd6aaf45e"),
			Signature: "-----BEGIN PGP SIGNATURE-----\n" +
				"\n" +
				"inlineSig=\n" +
				"-----END PGP SIGNATURE-----\n",
		},
		{
			// Compat mode in a SHA-1 primary repo: the SHA-256 sig is
			// embedded as a "gpgsig-sha256" header, and the SHA-1 sig
			// is appended inline. Mirrors the primary buffer produced
			// by builtin/tag.c:do_sign.
			Name:       "compat-primary-sha1",
			Tagger:     Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Message:    "Compat-mode tag, primary SHA-1\n",
			TargetType: plumbing.CommitObject,
			Target:     plumbing.NewHash("c029517f6300c2da0f4b651b8642506cd6aaf45e"),
			SignatureSHA256: "-----BEGIN PGP SIGNATURE-----\n" +
				"\n" +
				"sha256line1\n" +
				"sha256line2\n" +
				"-----END PGP SIGNATURE-----\n",
			Signature: "-----BEGIN PGP SIGNATURE-----\n" +
				"\n" +
				"inlineSHA1=\n" +
				"-----END PGP SIGNATURE-----\n",
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
		tag.src = obj
		s.Equal(tag, newTag)
	}
}

func (s *TagSuite) TestTagEncodeOmitsZeroTagger() {
	const raw = "object c029517f6300c2da0f4b651b8642506cd6aaf45e\n" +
		"type commit\n" +
		"tag v1\n" +
		"\n" +
		"msg\n"

	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.TagObject)
	_, err := obj.Write([]byte(raw))
	s.NoError(err)

	tag := &Tag{}
	s.NoError(tag.Decode(obj))
	s.Equal(Signature{}, tag.Tagger)

	encoded := &plumbing.MemoryObject{}
	s.NoError(tag.Encode(encoded))

	r, err := encoded.Reader()
	s.NoError(err)
	defer r.Close()

	body, err := io.ReadAll(r)
	s.NoError(err)
	s.Equal(raw, string(body))
}

func (s *TagSuite) TestTagDecodeClearsExistingState() {
	const raw = "object c029517f6300c2da0f4b651b8642506cd6aaf45e\n" +
		"type commit\n" +
		"tag fresh\n" +
		"\n" +
		"fresh message\n"

	store := memory.NewStorage()
	staleSrc := &plumbing.MemoryObject{}
	tag := &Tag{
		Hash:            plumbing.NewHash("1111111111111111111111111111111111111111"),
		Name:            "stale",
		Tagger:          Signature{Name: "Stale", Email: "stale@example.local", When: time.Unix(1, 0).UTC()},
		Message:         "stale message",
		Signature:       "stale signature",
		SignatureSHA256: "stale sha256 signature",
		TargetType:      plumbing.BlobObject,
		Target:          plumbing.NewHash("2222222222222222222222222222222222222222"),
		s:               store,
		src:             staleSrc,
	}

	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.TagObject)
	_, err := obj.Write([]byte(raw))
	s.NoError(err)

	s.NoError(tag.Decode(obj))
	s.Equal(obj.Hash(), tag.Hash)
	s.Equal("fresh", tag.Name)
	s.Equal(Signature{}, tag.Tagger)
	s.Equal("fresh message\n", tag.Message)
	s.Equal("", tag.Signature)
	s.Equal("", tag.SignatureSHA256)
	s.Equal(plumbing.CommitObject, tag.TargetType)
	s.Equal("c029517f6300c2da0f4b651b8642506cd6aaf45e", tag.Target.String())
	s.Equal(store, tag.s)
	s.Equal(obj, tag.src)
}

func (s *TagSuite) TestTagDecodeRoundTrip() {
	const (
		target  = "c029517f6300c2da0f4b651b8642506cd6aaf45e"
		tagger  = "Foo <foo@example.local> 1500000000 +0000"
		sha256B = "-----BEGIN PGP SIGNATURE-----\n\nsha256line1\nsha256line2\n-----END PGP SIGNATURE-----\n"
		inlineB = "-----BEGIN PGP SIGNATURE-----\n\ninlineline1\ninlineline2\n-----END PGP SIGNATURE-----\n"
	)
	headers := "object " + target + "\ntype commit\ntag t\ntagger " + tagger + "\n"
	sha256Block := "gpgsig-sha256 -----BEGIN PGP SIGNATURE-----\n" +
		" \n" +
		" sha256line1\n" +
		" sha256line2\n" +
		" -----END PGP SIGNATURE-----\n"

	tests := []struct {
		name   string
		raw    string
		assert func(*Tag)
	}{
		{
			name: "no signature",
			raw:  headers + "\nplain tag message\n",
			assert: func(t *Tag) {
				s.Empty(t.Signature)
				s.Empty(t.SignatureSHA256)
				s.Equal("plain tag message\n", t.Message)
			},
		},
		{
			name: "inline trailing PGP signature",
			raw:  headers + "\nTag body\n" + inlineB,
			assert: func(t *Tag) {
				s.Equal("Tag body\n", t.Message)
				s.Equal(inlineB, t.Signature)
				s.Empty(t.SignatureSHA256)
			},
		},
		{
			// Synthetic: upstream's do_sign always pairs the
			// gpgsig-sha256 header with an inline trailing
			// signature. Kept here to confirm the decoder/encoder
			// don't depend on the trailer being present.
			name: "gpgsig-sha256 header only (synthetic, no inline trailer)",
			raw:  headers + sha256Block + "\nTag body, header sig only\n",
			assert: func(t *Tag) {
				s.Equal("Tag body, header sig only\n", t.Message)
				s.Equal(sha256B, t.SignatureSHA256)
				s.Empty(t.Signature)
			},
		},
		{
			// Real compat-mode layout for a SHA-1 primary repo: the
			// SHA-256 sig is embedded as a "gpgsig-sha256" header,
			// and the SHA-1 sig is appended inline.
			name: "compat-mode, primary SHA-1 (gpgsig-sha256 header + inline trailer)",
			raw:  headers + sha256Block + "\nDual-signed tag body\n" + inlineB,
			assert: func(t *Tag) {
				s.Equal("Dual-signed tag body\n", t.Message)
				s.Equal(sha256B, t.SignatureSHA256)
				s.Equal(inlineB, t.Signature)
			},
		},
		{
			// parseSignedBytes returns the position of the LAST PGP
			// block in the buffer, mirroring upstream's
			// parse_signed_buffer (gpg-interface.c:702). Any earlier
			// PGP-armored bytes embedded in the body stay part of the
			// message; only the trailing block becomes t.Signature.
			name: "multiple inline PGP blocks: last is the signature",
			raw: headers + "\nbody line 1\n" +
				"-----BEGIN PGP SIGNATURE-----\nfakeline\n-----END PGP SIGNATURE-----\n" +
				"body line 2\n" +
				"-----BEGIN PGP SIGNATURE-----\nrealline\n-----END PGP SIGNATURE-----\n",
			assert: func(t *Tag) {
				s.Equal(
					"body line 1\n"+
						"-----BEGIN PGP SIGNATURE-----\nfakeline\n-----END PGP SIGNATURE-----\n"+
						"body line 2\n",
					t.Message,
				)
				s.Equal(
					"-----BEGIN PGP SIGNATURE-----\nrealline\n-----END PGP SIGNATURE-----\n",
					t.Signature,
				)
				s.Empty(t.SignatureSHA256)
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			obj := &plumbing.MemoryObject{}
			obj.SetType(plumbing.TagObject)
			_, err := obj.Write([]byte(tc.raw))
			s.NoError(err)

			tag := &Tag{}
			s.Require().NoError(tag.Decode(obj))
			tc.assert(tag)

			encoded := &plumbing.MemoryObject{}
			s.NoError(tag.Encode(encoded))
			er, err := encoded.Reader()
			s.NoError(err)
			roundTripped, err := io.ReadAll(er)
			s.NoError(err)
			s.Equal(tc.raw, string(roundTripped))
		})
	}
}

func (s *TagSuite) TestDecodeRequiresHeaders() {
	const (
		target = "c029517f6300c2da0f4b651b8642506cd6aaf45e"
		tagger = "Foo <foo@example.local> 1500000000 +0000"
	)

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "empty",
			raw:  "",
		},
		{
			name: "missing object",
			raw:  "type commit\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "object out of canonical position",
			raw:  "type commit\nobject " + target + "\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "missing type",
			raw:  "object " + target + "\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "missing tag",
			raw:  "object " + target + "\ntype commit\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "duplicate object before type",
			raw: "object " + target + "\nobject " + target +
				"\ntype commit\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "duplicate type before tag",
			raw: "object " + target + "\ntype commit\ntype blob" +
				"\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "truncated after object header",
			raw:  "object " + target + "\n",
		},
		{
			name: "truncated after type header",
			raw:  "object " + target + "\ntype commit\n",
		},
		{
			name: "non-hex object value",
			raw:  "object zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz\ntype commit\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "object value too short",
			raw:  "object abcd\ntype commit\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "object value too long",
			raw:  "object " + target + "00\ntype commit\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
		{
			name: "object value missing",
			raw:  "object\ntype commit\ntag v1\ntagger " + tagger + "\n\nmsg\n",
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			obj := &plumbing.MemoryObject{}
			obj.SetType(plumbing.TagObject)
			_, err := obj.Write([]byte(tc.raw))
			s.NoError(err)

			err = (&Tag{}).Decode(obj)
			s.ErrorIs(err, ErrMalformedTag)
		})
	}
}

func (s *TagSuite) TestDecodeFirstOccurrenceWins() {
	const (
		targetA   = "c029517f6300c2da0f4b651b8642506cd6aaf45e"
		taggerA   = "Alice <alice@example.local> 1500000000 +0000"
		taggerB   = "Bob <bob@example.local> 1500000001 +0000"
		canonical = "object " + targetA +
			"\ntype commit\ntag v1\ntagger " + taggerA + "\n"
	)

	cases := []struct {
		name   string
		raw    string
		assert func(*Tag)
	}{
		{
			name: "duplicate tag drops the second",
			raw: "object " + targetA + "\ntype commit\ntag v1\ntag v1-override" +
				"\ntagger " + taggerA + "\n\nmsg\n",
			assert: func(t *Tag) {
				s.Equal("v1", t.Name)
			},
		},
		{
			name: "duplicate tagger drops the second",
			raw: "object " + targetA + "\ntype commit\ntag v1\ntagger " + taggerA +
				"\ntagger " + taggerB + "\n\nmsg\n",
			assert: func(t *Tag) {
				s.Equal("Alice", t.Tagger.Name)
			},
		},
		{
			name: "missing tagger is allowed (zero-valued)",
			raw:  "object " + targetA + "\ntype commit\ntag v1\n\nmsg\n",
			assert: func(t *Tag) {
				s.Equal("v1", t.Name)
				s.Empty(t.Tagger.Name)
				s.Equal("msg\n", t.Message)
			},
		},
		{
			// gpgsig-sha256 headers concatenate, mirroring upstream's
			// parse_buffer_signed_by_header (commit.c:1186) — same
			// behaviour the commit scanner has for gpgsig.
			name: "multiple gpgsig-sha256 headers concatenate",
			raw: canonical +
				"gpgsig-sha256 firstline\n morefirst\n" +
				"gpgsig-sha256 secondline\n moresecond\n\nmsg\n",
			assert: func(t *Tag) {
				s.Equal("firstline\nmorefirst\nsecondline\nmoresecond\n", t.SignatureSHA256)
			},
		},
		{
			name: "single-line gpgsig-sha256 (no continuation)",
			raw:  canonical + "gpgsig-sha256 short\n\nmsg\n",
			assert: func(t *Tag) {
				s.Equal("short\n", t.SignatureSHA256)
			},
		},
		{
			name: "gpgsig-sha256 with empty value",
			raw:  canonical + "gpgsig-sha256\n\nmsg\n",
			assert: func(t *Tag) {
				s.Equal("\n", t.SignatureSHA256)
			},
		},
		{
			name: "unknown extra header is dropped",
			raw:  canonical + "x-some-extra value\n\nmsg\n",
			assert: func(t *Tag) {
				s.Equal(targetA, t.Target.String())
				s.Equal("v1", t.Name)
				s.Equal("Alice", t.Tagger.Name)
				s.Equal("msg\n", t.Message)
				s.Empty(t.Signature)
				s.Empty(t.SignatureSHA256)
			},
		},
		{
			name: "EOF after tagger (no blank-line separator)",
			raw:  "object " + targetA + "\ntype commit\ntag v1\ntagger " + taggerA + "\n",
			assert: func(t *Tag) {
				s.Equal(targetA, t.Target.String())
				s.Equal(plumbing.CommitObject, t.TargetType)
				s.Equal("v1", t.Name)
				s.Equal("Alice", t.Tagger.Name)
				s.Empty(t.Message)
			},
		},
		{
			name: "EOF on blank line (empty body)",
			raw:  "object " + targetA + "\ntype commit\ntag v1\ntagger " + taggerA + "\n\n",
			assert: func(t *Tag) {
				s.Equal("v1", t.Name)
				s.Empty(t.Message)
			},
		},
		{
			name: "message without trailing newline",
			raw:  "object " + targetA + "\ntype commit\ntag v1\ntagger " + taggerA + "\n\npartial",
			assert: func(t *Tag) {
				s.Equal("partial", t.Message)
			},
		},
		{
			name: "EOF mid-gpgsig-sha256 continuation",
			raw:  canonical + "gpgsig-sha256 line1\n line2\n line3\n",
			assert: func(t *Tag) {
				s.Equal("line1\nline2\nline3\n", t.SignatureSHA256)
				s.Empty(t.Message)
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			obj := &plumbing.MemoryObject{}
			obj.SetType(plumbing.TagObject)
			_, err := obj.Write([]byte(tc.raw))
			s.NoError(err)

			tag := &Tag{}
			s.Require().NoError(tag.Decode(obj))
			tc.assert(tag)
		})
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
		Tagger:     Signature{Name: "Test Tagger", Email: "tagger@example.local", When: time.Unix(0, 0).UTC()},
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
			"Tagger: Test Tagger <tagger@example.local>\n"+
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
	s.NoError(err)

	_, ok := e.Identities["go-git test key"]
	s.True(ok)
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

	armoredKeyRing := `
-----BEGIN PGP PUBLIC KEY BLOCK-----

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
=oYTT
-----END PGP PUBLIC KEY BLOCK-----
`

	tagEncodedObject := &plumbing.MemoryObject{}

	_, err := tagEncodedObject.Write([]byte(objectText))
	tagEncodedObject.SetType(plumbing.TagObject)
	s.NoError(err)

	tag := &Tag{}
	err = tag.Decode(tagEncodedObject)
	s.NoError(err)

	_, err = tag.Verify(armoredKeyRing)
	s.NoError(err)
}

func (s *TagSuite) TestEncodeWithoutSignature() {
	tests := []struct {
		name     string
		tagRaw   string
		mutate   func(*Tag)
		expected string
	}{
		{
			name: "tag without signature",
			tagRaw: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0000

plain tag message
`,
			expected: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0000

plain tag message
`,
		},
		{
			name: "gpgsig-sha256 header",
			tagRaw: "object 1eca38290a3131d0c90709496a9b2207a872631e\n" +
				"type commit\n" +
				"tag v1\n" +
				"tagger Test Tagger <tagger@example.local> 1700000000 +0000\n" +
				"gpgsig-sha256 -----BEGIN PGP SIGNATURE-----\n" +
				" \n" +
				" sha256line1\n" +
				" sha256line2\n" +
				" -----END PGP SIGNATURE-----\n" +
				"\n" +
				"tag message\n",
			expected: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0000

tag message
`,
		},
		{
			name: "inline trailing signature",
			tagRaw: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0000

tag message
-----BEGIN PGP SIGNATURE-----

inlineline1
inlineline2
-----END PGP SIGNATURE-----
`,
			expected: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0000

tag message
`,
		},
		{
			// Compat-mode primary SHA-1 layout: gpgsig-sha256 header
			// (SHA-256 sig) plus inline trailing PGP block (SHA-1 sig).
			// Both are stripped to produce the verification payload.
			name: "gpgsig-sha256 + inline trailing",
			tagRaw: "object 1eca38290a3131d0c90709496a9b2207a872631e\n" +
				"type commit\n" +
				"tag v1\n" +
				"tagger Test Tagger <tagger@example.local> 1700000000 +0000\n" +
				"gpgsig-sha256 -----BEGIN PGP SIGNATURE-----\n" +
				" \n" +
				" sha256line1\n" +
				" -----END PGP SIGNATURE-----\n" +
				"\n" +
				"tag message\n" +
				"-----BEGIN PGP SIGNATURE-----\n" +
				"\n" +
				"inlineline1\n" +
				"-----END PGP SIGNATURE-----\n",
			expected: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0000

tag message
`,
		},
		{
			// Mutating only the signature fields must not trigger
			// struct-encode: the raw bytes (with both signatures
			// stripped) are still the canonical signed payload.
			name: "raw bytes preserved when only signatures are mutated",
			tagRaw: "object 1eca38290a3131d0c90709496a9b2207a872631e\n" +
				"type commit\n" +
				"tag v1\n" +
				"tagger Test Tagger <tagger@example.local> 1700000000 +0000\n" +
				"gpgsig-sha256 -----BEGIN PGP SIGNATURE-----\n" +
				" sha256line1\n" +
				" -----END PGP SIGNATURE-----\n" +
				"\n" +
				"tag message\n" +
				"-----BEGIN PGP SIGNATURE-----\n" +
				"inlineline1\n" +
				"-----END PGP SIGNATURE-----\n",
			mutate: func(t *Tag) {
				t.Signature = "different signature value"
				t.SignatureSHA256 = "different sha256 sig"
			},
			expected: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0000

tag message
`,
		},
		{
			name: "timezone-only change triggers struct-encode",
			tagRaw: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 -0700

tag message
-----BEGIN PGP SIGNATURE-----
inlineline1
-----END PGP SIGNATURE-----
`,
			mutate: func(t *Tag) {
				tz := time.FixedZone("CEST", 2*60*60)
				t.Tagger.When = t.Tagger.When.In(tz)
			},
			expected: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0200

tag message
`,
		},
		{
			name: "field mutation triggers struct-encode",
			tagRaw: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1
tagger Test Tagger <tagger@example.local> 1700000000 +0000

tag message
-----BEGIN PGP SIGNATURE-----
inlineline1
-----END PGP SIGNATURE-----
`,
			mutate: func(t *Tag) {
				t.Name = "v1-rewritten"
			},
			expected: `object 1eca38290a3131d0c90709496a9b2207a872631e
type commit
tag v1-rewritten
tagger Test Tagger <tagger@example.local> 1700000000 +0000

tag message
`,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			obj := &plumbing.MemoryObject{}
			obj.SetType(plumbing.TagObject)
			_, err := obj.Write([]byte(tc.tagRaw))
			s.Require().NoError(err)

			tag := &Tag{}
			s.Require().NoError(tag.Decode(obj))

			if tc.mutate != nil {
				tc.mutate(tag)
			}

			encoded := &plumbing.MemoryObject{}
			err = tag.EncodeWithoutSignature(encoded)
			s.NoError(err)

			er, err := encoded.Reader()
			s.NoError(err)

			payload, err := io.ReadAll(er)
			s.NoError(err)

			s.Equal(tc.expected, string(payload))
		})
	}
}
