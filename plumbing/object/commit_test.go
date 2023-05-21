package object

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"

	"github.com/go-git/go-git/v5/storage/filesystem"
	. "gopkg.in/check.v1"
)

type SuiteCommit struct {
	BaseObjectsSuite
	Commit *Commit
}

var _ = Suite(&SuiteCommit{})

func (s *SuiteCommit) SetUpSuite(c *C) {
	s.BaseObjectsSuite.SetUpSuite(c)

	hash := plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")

	s.Commit = s.commit(c, hash)
}

func (s *SuiteCommit) TestDecodeNonCommit(c *C) {
	hash := plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")
	blob, err := s.Storer.EncodedObject(plumbing.AnyObject, hash)
	c.Assert(err, IsNil)

	commit := &Commit{}
	err = commit.Decode(blob)
	c.Assert(err, Equals, ErrUnsupportedObject)
}

func (s *SuiteCommit) TestType(c *C) {
	c.Assert(s.Commit.Type(), Equals, plumbing.CommitObject)
}

func (s *SuiteCommit) TestTree(c *C) {
	tree, err := s.Commit.Tree()
	c.Assert(err, IsNil)
	c.Assert(tree.ID().String(), Equals, "eba74343e2f15d62adedfd8c883ee0262b5c8021")
}

func (s *SuiteCommit) TestParents(c *C) {
	expected := []string{
		"35e85108805c84807bc66a02d91535e1e24b38b9",
		"a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69",
	}

	var output []string
	i := s.Commit.Parents()
	err := i.ForEach(func(commit *Commit) error {
		output = append(output, commit.ID().String())
		return nil
	})

	c.Assert(err, IsNil)
	c.Assert(output, DeepEquals, expected)

	i.Close()
}

func (s *SuiteCommit) TestParent(c *C) {
	commit, err := s.Commit.Parent(1)
	c.Assert(err, IsNil)
	c.Assert(commit.Hash.String(), Equals, "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69")
}

func (s *SuiteCommit) TestParentNotFound(c *C) {
	commit, err := s.Commit.Parent(42)
	c.Assert(err, Equals, ErrParentNotFound)
	c.Assert(commit, IsNil)
}

func (s *SuiteCommit) TestPatch(c *C) {
	from := s.commit(c, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	to := s.commit(c, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	patch, err := from.Patch(to)
	c.Assert(err, IsNil)

	buf := bytes.NewBuffer(nil)
	err = patch.Encode(buf)
	c.Assert(err, IsNil)

	c.Assert(buf.String(), Equals, `diff --git a/vendor/foo.go b/vendor/foo.go
new file mode 100644
index 0000000000000000000000000000000000000000..9dea2395f5403188298c1dabe8bdafe562c491e3
--- /dev/null
+++ b/vendor/foo.go
@@ -0,0 +1,7 @@
+package main
+
+import "fmt"
+
+func main() {
+	fmt.Println("Hello, playground")
+}
`)
	c.Assert(buf.String(), Equals, patch.String())

	from = s.commit(c, plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"))
	to = s.commit(c, plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"))

	patch, err = from.Patch(to)
	c.Assert(err, IsNil)

	buf.Reset()
	err = patch.Encode(buf)
	c.Assert(err, IsNil)

	c.Assert(buf.String(), Equals, `diff --git a/CHANGELOG b/CHANGELOG
deleted file mode 100644
index d3ff53e0564a9f87d8e84b6e28e5060e517008aa..0000000000000000000000000000000000000000
--- a/CHANGELOG
+++ /dev/null
@@ -1 +0,0 @@
-Initial changelog
diff --git a/binary.jpg b/binary.jpg
new file mode 100644
index 0000000000000000000000000000000000000000..d5c0f4ab811897cadf03aec358ae60d21f91c50d
Binary files /dev/null and b/binary.jpg differ
`)

	c.Assert(buf.String(), Equals, patch.String())
}

func (s *SuiteCommit) TestPatchContext(c *C) {
	from := s.commit(c, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	to := s.commit(c, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	patch, err := from.PatchContext(context.Background(), to)
	c.Assert(err, IsNil)

	buf := bytes.NewBuffer(nil)
	err = patch.Encode(buf)
	c.Assert(err, IsNil)

	c.Assert(buf.String(), Equals, `diff --git a/vendor/foo.go b/vendor/foo.go
new file mode 100644
index 0000000000000000000000000000000000000000..9dea2395f5403188298c1dabe8bdafe562c491e3
--- /dev/null
+++ b/vendor/foo.go
@@ -0,0 +1,7 @@
+package main
+
+import "fmt"
+
+func main() {
+	fmt.Println("Hello, playground")
+}
`)
	c.Assert(buf.String(), Equals, patch.String())

	from = s.commit(c, plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"))
	to = s.commit(c, plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"))

	patch, err = from.PatchContext(context.Background(), to)
	c.Assert(err, IsNil)

	buf.Reset()
	err = patch.Encode(buf)
	c.Assert(err, IsNil)

	c.Assert(buf.String(), Equals, `diff --git a/CHANGELOG b/CHANGELOG
deleted file mode 100644
index d3ff53e0564a9f87d8e84b6e28e5060e517008aa..0000000000000000000000000000000000000000
--- a/CHANGELOG
+++ /dev/null
@@ -1 +0,0 @@
-Initial changelog
diff --git a/binary.jpg b/binary.jpg
new file mode 100644
index 0000000000000000000000000000000000000000..d5c0f4ab811897cadf03aec358ae60d21f91c50d
Binary files /dev/null and b/binary.jpg differ
`)

	c.Assert(buf.String(), Equals, patch.String())
}

func (s *SuiteCommit) TestPatchContext_ToNil(c *C) {
	from := s.commit(c, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))

	patch, err := from.PatchContext(context.Background(), nil)
	c.Assert(err, IsNil)

	c.Assert(len(patch.String()), Equals, 242679)
}

func (s *SuiteCommit) TestCommitEncodeDecodeIdempotent(c *C) {
	ts, err := time.Parse(time.RFC3339, "2006-01-02T15:04:05-07:00")
	c.Assert(err, IsNil)
	commits := []*Commit{
		{
			Author:       Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Committer:    Signature{Name: "Bar", Email: "bar@example.local", When: ts},
			Message:      "Message\n\nFoo\nBar\nWith trailing blank lines\n\n",
			TreeHash:     plumbing.NewHash("f000000000000000000000000000000000000001"),
			ParentHashes: []plumbing.Hash{plumbing.NewHash("f000000000000000000000000000000000000002")},
		},
		{
			Author:    Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Committer: Signature{Name: "Bar", Email: "bar@example.local", When: ts},
			Message:   "Message\n\nFoo\nBar\nWith no trailing blank lines",
			TreeHash:  plumbing.NewHash("0000000000000000000000000000000000000003"),
			ParentHashes: []plumbing.Hash{
				plumbing.NewHash("f000000000000000000000000000000000000004"),
				plumbing.NewHash("f000000000000000000000000000000000000005"),
				plumbing.NewHash("f000000000000000000000000000000000000006"),
				plumbing.NewHash("f000000000000000000000000000000000000007"),
			},
		},
	}
	for _, commit := range commits {
		obj := &plumbing.MemoryObject{}
		err = commit.Encode(obj)
		c.Assert(err, IsNil)
		newCommit := &Commit{}
		err = newCommit.Decode(obj)
		c.Assert(err, IsNil)
		commit.Hash = obj.Hash()
		c.Assert(newCommit, DeepEquals, commit)
	}
}

func (s *SuiteCommit) TestFile(c *C) {
	file, err := s.Commit.File("CHANGELOG")
	c.Assert(err, IsNil)
	c.Assert(file.Name, Equals, "CHANGELOG")
}

func (s *SuiteCommit) TestNumParents(c *C) {
	c.Assert(s.Commit.NumParents(), Equals, 2)
}

func (s *SuiteCommit) TestString(c *C) {
	c.Assert(s.Commit.String(), Equals, ""+
		"commit 1669dce138d9b841a518c64b10914d88f5e488ea\n"+
		"Author: Máximo Cuadros Ortiz <mcuadros@gmail.com>\n"+
		"Date:   Tue Mar 31 13:48:14 2015 +0200\n"+
		"\n"+
		"    Merge branch 'master' of github.com:tyba/git-fixture\n"+
		"\n",
	)
}

func (s *SuiteCommit) TestStringMultiLine(c *C) {
	hash := plumbing.NewHash("e7d896db87294e33ca3202e536d4d9bb16023db3")

	f := fixtures.ByURL("https://github.com/src-d/go-git.git").One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	o, err := sto.EncodedObject(plumbing.CommitObject, hash)
	c.Assert(err, IsNil)
	commit, err := DecodeCommit(sto, o)
	c.Assert(err, IsNil)

	c.Assert(commit.String(), Equals, ""+
		"commit e7d896db87294e33ca3202e536d4d9bb16023db3\n"+
		"Author: Alberto Cortés <alberto@sourced.tech>\n"+
		"Date:   Wed Jan 27 11:13:49 2016 +0100\n"+
		"\n"+
		"    fix zlib invalid header error\n"+
		"\n"+
		"    The return value of reads to the packfile were being ignored, so zlib\n"+
		"    was getting invalid data on it read buffers.\n"+
		"\n",
	)
}

func (s *SuiteCommit) TestCommitIterNext(c *C) {
	i := s.Commit.Parents()

	commit, err := i.Next()
	c.Assert(err, IsNil)
	c.Assert(commit.ID().String(), Equals, "35e85108805c84807bc66a02d91535e1e24b38b9")

	commit, err = i.Next()
	c.Assert(err, IsNil)
	c.Assert(commit.ID().String(), Equals, "a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69")

	commit, err = i.Next()
	c.Assert(err, Equals, io.EOF)
	c.Assert(commit, IsNil)
}

func (s *SuiteCommit) TestLongCommitMessageSerialization(c *C) {
	encoded := &plumbing.MemoryObject{}
	decoded := &Commit{}
	commit := *s.Commit

	longMessage := "my message: message\n\n" + strings.Repeat("test", 4096) + "\nOK"
	commit.Message = longMessage

	err := commit.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
	c.Assert(decoded.Message, Equals, longMessage)
}

func (s *SuiteCommit) TestPGPSignatureSerialization(c *C) {
	encoded := &plumbing.MemoryObject{}
	decoded := &Commit{}
	commit := *s.Commit

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
	commit.PGPSignature = pgpsignature

	err := commit.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
	c.Assert(decoded.PGPSignature, Equals, pgpsignature)

	// signature with extra empty line, it caused "index out of range" when
	// parsing it

	pgpsignature2 := "\n" + pgpsignature

	commit.PGPSignature = pgpsignature2
	encoded = &plumbing.MemoryObject{}
	decoded = &Commit{}

	err = commit.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
	c.Assert(decoded.PGPSignature, Equals, pgpsignature2)

	// signature in author name

	commit.PGPSignature = ""
	commit.Author.Name = beginpgp
	encoded = &plumbing.MemoryObject{}
	decoded = &Commit{}

	err = commit.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
	c.Assert(decoded.PGPSignature, Equals, "")
	c.Assert(decoded.Author.Name, Equals, beginpgp)

	// broken signature

	commit.PGPSignature = beginpgp + "\n" +
		"some\n" +
		"trash\n" +
		endpgp +
		"text\n"
	encoded = &plumbing.MemoryObject{}
	decoded = &Commit{}

	err = commit.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
	c.Assert(decoded.PGPSignature, Equals, commit.PGPSignature)
}

func (s *SuiteCommit) TestStat(c *C) {
	aCommit := s.commit(c, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	fileStats, err := aCommit.Stats()
	c.Assert(err, IsNil)

	c.Assert(fileStats[0].Name, Equals, "vendor/foo.go")
	c.Assert(fileStats[0].Addition, Equals, 7)
	c.Assert(fileStats[0].Deletion, Equals, 0)
	c.Assert(fileStats[0].String(), Equals, " vendor/foo.go | 7 +++++++\n")

	// Stats for another commit.
	aCommit = s.commit(c, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	fileStats, err = aCommit.Stats()
	c.Assert(err, IsNil)

	c.Assert(fileStats[0].Name, Equals, "go/example.go")
	c.Assert(fileStats[0].Addition, Equals, 142)
	c.Assert(fileStats[0].Deletion, Equals, 0)
	c.Assert(fileStats[0].String(), Equals, " go/example.go | 142 +++++++++++++++++++++++++++++++++++++++++++++++++++++\n")

	c.Assert(fileStats[1].Name, Equals, "php/crappy.php")
	c.Assert(fileStats[1].Addition, Equals, 259)
	c.Assert(fileStats[1].Deletion, Equals, 0)
	c.Assert(fileStats[1].String(), Equals, " php/crappy.php | 259 ++++++++++++++++++++++++++++++++++++++++++++++++++++\n")
}

func (s *SuiteCommit) TestVerify(c *C) {
	ts := time.Unix(1617402711, 0)
	loc, _ := time.LoadLocation("UTC")
	commit := &Commit{
		Hash:      plumbing.NewHash("1eca38290a3131d0c90709496a9b2207a872631e"),
		Author:    Signature{Name: "go-git", Email: "go-git@example.com", When: ts.In(loc)},
		Committer: Signature{Name: "go-git", Email: "go-git@example.com", When: ts.In(loc)},
		Message: `test
`,
		TreeHash:     plumbing.NewHash("52a266a58f2c028ad7de4dfd3a72fdf76b0d4e24"),
		ParentHashes: []plumbing.Hash{plumbing.NewHash("e4fbb611cd14149c7a78e9c08425f59f4b736a9a")},
		PGPSignature: `
-----BEGIN PGP SIGNATURE-----

iHUEABYKAB0WIQTMqU0ycQ3f6g3PMoWMmmmF4LuV8QUCYGebVwAKCRCMmmmF4LuV
8VtyAP9LbuXAhtK6FQqOjKybBwlV70rLcXVP24ubDuz88VVwSgD+LuObsasWq6/U
TssDKHUR2taa53bQYjkZQBpvvwOrLgc=
=YQUf
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

	e, err := commit.Verify(armoredKeyRing)
	c.Assert(err, IsNil)

	_, ok := e.Identities["go-git test key"]
	c.Assert(ok, Equals, true)
}

func (s *SuiteCommit) TestPatchCancel(c *C) {
	from := s.commit(c, plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	to := s.commit(c, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	patch, err := from.PatchContext(ctx, to)
	c.Assert(patch, IsNil)
	c.Assert(err, ErrorMatches, "operation canceled")

}

func (s *SuiteCommit) TestMalformedHeader(c *C) {
	encoded := &plumbing.MemoryObject{}
	decoded := &Commit{}
	commit := *s.Commit

	commit.PGPSignature = "\n"
	commit.Author.Name = "\n"
	commit.Author.Email = "\n"
	commit.Committer.Name = "\n"
	commit.Committer.Email = "\n"

	err := commit.Encode(encoded)
	c.Assert(err, IsNil)

	err = decoded.Decode(encoded)
	c.Assert(err, IsNil)
}

func (s *SuiteCommit) TestEncodeWithoutSignature(c *C) {
	//Similar to TestString since no signature
	encoded := &plumbing.MemoryObject{}
	err := s.Commit.EncodeWithoutSignature(encoded)
	c.Assert(err, IsNil)
	er, err := encoded.Reader()
	c.Assert(err, IsNil)
	payload, err := io.ReadAll(er)
	c.Assert(err, IsNil)

	c.Assert(string(payload), Equals, ""+
		"tree eba74343e2f15d62adedfd8c883ee0262b5c8021\n"+
		"parent 35e85108805c84807bc66a02d91535e1e24b38b9\n"+
		"parent a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69\n"+
		"author Máximo Cuadros Ortiz <mcuadros@gmail.com> 1427802494 +0200\n"+
		"committer Máximo Cuadros Ortiz <mcuadros@gmail.com> 1427802494 +0200\n"+
		"\n"+
		"Merge branch 'master' of github.com:tyba/git-fixture\n")
}
