package object

import (
	"bytes"
	"context"
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
)

type SuiteCommit struct {
	suite.Suite
	BaseObjectsSuite
	Commit *Commit
}

func TestSuiteCommit(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SuiteCommit))
}

func (s *SuiteCommit) SetupSuite() {
	s.BaseObjectsSuite.SetupSuite(s.T())

	hash := plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")

	s.Commit = s.commit(hash)
}

func (s *SuiteCommit) TestDecodeNonCommit() {
	hash := plumbing.NewHash("9a48f23120e880dfbe41f7c9b7b708e9ee62a492")
	blob, err := s.Storer.EncodedObject(plumbing.AnyObject, hash)
	s.NoError(err)

	commit := &Commit{}
	err = commit.Decode(blob)
	s.ErrorIs(err, ErrUnsupportedObject)
}

func (s *SuiteCommit) TestType() {
	s.Equal(plumbing.CommitObject, s.Commit.Type())
}

func (s *SuiteCommit) TestTree() {
	tree, err := s.Commit.Tree()
	s.NoError(err)
	s.Equal("eba74343e2f15d62adedfd8c883ee0262b5c8021", tree.ID().String())
}

func (s *SuiteCommit) TestParents() {
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

	s.NoError(err)
	s.Equal(expected, output)

	i.Close()
}

func (s *SuiteCommit) TestParent() {
	commit, err := s.Commit.Parent(1)
	s.NoError(err)
	s.Equal("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69", commit.Hash.String())
}

func (s *SuiteCommit) TestParentNotFound() {
	commit, err := s.Commit.Parent(42)
	s.ErrorIs(err, ErrParentNotFound)
	s.Nil(commit)
}

func (s *SuiteCommit) TestPatch() {
	from := s.commit(plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	to := s.commit(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	patch, err := from.Patch(to)
	s.NoError(err)

	buf := bytes.NewBuffer(nil)
	err = patch.Encode(buf)
	s.NoError(err)

	s.Equal(`diff --git a/vendor/foo.go b/vendor/foo.go
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
`,
		buf.String())
	s.Equal(patch.String(), buf.String())

	from = s.commit(plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"))
	to = s.commit(plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"))

	patch, err = from.Patch(to)
	s.NoError(err)

	buf.Reset()
	err = patch.Encode(buf)
	s.NoError(err)

	s.Equal(`diff --git a/CHANGELOG b/CHANGELOG
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
`,
		buf.String())

	s.Equal(patch.String(), buf.String())
}

func (s *SuiteCommit) TestPatchContext() {
	from := s.commit(plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	to := s.commit(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	patch, err := from.PatchContext(context.Background(), to)
	s.NoError(err)

	buf := bytes.NewBuffer(nil)
	err = patch.Encode(buf)
	s.NoError(err)

	s.Equal(`diff --git a/vendor/foo.go b/vendor/foo.go
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
`,
		buf.String())
	s.Equal(patch.String(), buf.String())

	from = s.commit(plumbing.NewHash("b8e471f58bcbca63b07bda20e428190409c2db47"))
	to = s.commit(plumbing.NewHash("35e85108805c84807bc66a02d91535e1e24b38b9"))

	patch, err = from.PatchContext(context.Background(), to)
	s.NoError(err)

	buf.Reset()
	err = patch.Encode(buf)
	s.NoError(err)

	s.Equal(`diff --git a/CHANGELOG b/CHANGELOG
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
`,
		buf.String())

	s.Equal(patch.String(), buf.String())
}

func (s *SuiteCommit) TestPatchContext_ToNil() {
	from := s.commit(plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))

	patch, err := from.PatchContext(context.Background(), nil)
	s.NoError(err)

	s.Equal(242679, len(patch.String()))
}

func (s *SuiteCommit) TestCommitEncodeDecodeIdempotent() {
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

	tag := fmt.Sprintf(`object f000000000000000000000000000000000000000
type commit
tag change
tagger Foo <foo@example.local> 1695827841 -0400

change
%s
`, pgpsignature)

	ts, err := time.ParseInLocation(time.RFC3339, "2006-01-02T15:04:05-07:00", time.UTC)
	s.NoError(err)

	commits := []*Commit{
		{
			Author:       Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Committer:    Signature{Name: "Bar", Email: "bar@example.local", When: ts},
			Message:      "Message\n\nFoo\nBar\nWith trailing blank lines\n\n",
			TreeHash:     plumbing.NewHash("f000000000000000000000000000000000000001"),
			ParentHashes: []plumbing.Hash{plumbing.NewHash("f000000000000000000000000000000000000002")},
			Encoding:     defaultUtf8CommitMessageEncoding,
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
			Encoding: MessageEncoding("ISO-8859-1"),
		},
		{
			Author:    Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Committer: Signature{Name: "Bar", Email: "bar@example.local", When: ts},
			Message:   "Testing mergetag\n\nHere, commit is not signed",
			TreeHash:  plumbing.NewHash("f000000000000000000000000000000000000001"),
			ParentHashes: []plumbing.Hash{
				plumbing.NewHash("f000000000000000000000000000000000000002"),
				plumbing.NewHash("f000000000000000000000000000000000000003"),
			},
			MergeTag: tag,
			Encoding: defaultUtf8CommitMessageEncoding,
		},
		{
			Author:    Signature{Name: "Foo", Email: "foo@example.local", When: ts},
			Committer: Signature{Name: "Bar", Email: "bar@example.local", When: ts},
			Message:   "Testing mergetag\n\nHere, commit is also signed",
			TreeHash:  plumbing.NewHash("f000000000000000000000000000000000000001"),
			ParentHashes: []plumbing.Hash{
				plumbing.NewHash("f000000000000000000000000000000000000002"),
				plumbing.NewHash("f000000000000000000000000000000000000003"),
			},
			MergeTag:     tag,
			PGPSignature: pgpsignature,
			Encoding:     defaultUtf8CommitMessageEncoding,
		},
	}
	for _, commit := range commits {
		obj := &plumbing.MemoryObject{}
		err := commit.Encode(obj)
		s.NoError(err)
		newCommit := &Commit{}
		err = newCommit.Decode(obj)
		s.NoError(err)
		commit.Hash = obj.Hash()
		s.Equal(commit, newCommit)
	}
}

func (s *SuiteCommit) TestFile() {
	file, err := s.Commit.File("CHANGELOG")
	s.NoError(err)
	s.Equal("CHANGELOG", file.Name)
}

func (s *SuiteCommit) TestNumParents() {
	s.Equal(2, s.Commit.NumParents())
}

func (s *SuiteCommit) TestString() {
	s.Equal(""+
		"commit 1669dce138d9b841a518c64b10914d88f5e488ea\n"+
		"Author: Máximo Cuadros Ortiz <mcuadros@gmail.com>\n"+
		"Date:   Tue Mar 31 13:48:14 2015 +0200\n"+
		"\n"+
		"    Merge branch 'master' of github.com:tyba/git-fixture\n"+
		"\n",
		s.Commit.String(),
	)
}

func (s *SuiteCommit) TestStringMultiLine() {
	hash := plumbing.NewHash("e7d896db87294e33ca3202e536d4d9bb16023db3")

	f := fixtures.ByURL("https://github.com/src-d/go-git.git").One()
	sto := filesystem.NewStorage(f.DotGit(), cache.NewObjectLRUDefault())

	o, err := sto.EncodedObject(plumbing.CommitObject, hash)
	s.NoError(err)
	commit, err := DecodeCommit(sto, o)
	s.NoError(err)

	s.Equal(""+
		"commit e7d896db87294e33ca3202e536d4d9bb16023db3\n"+
		"Author: Alberto Cortés <alberto@sourced.tech>\n"+
		"Date:   Wed Jan 27 11:13:49 2016 +0100\n"+
		"\n"+
		"    fix zlib invalid header error\n"+
		"\n"+
		"    The return value of reads to the packfile were being ignored, so zlib\n"+
		"    was getting invalid data on it read buffers.\n"+
		"\n",
		commit.String(),
	)
}

func (s *SuiteCommit) TestCommitIterNext() {
	i := s.Commit.Parents()

	commit, err := i.Next()
	s.NoError(err)
	s.Equal("35e85108805c84807bc66a02d91535e1e24b38b9", commit.ID().String())

	commit, err = i.Next()
	s.NoError(err)
	s.Equal("a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69", commit.ID().String())

	commit, err = i.Next()
	s.ErrorIs(err, io.EOF)
	s.Nil(commit)
}

func (s *SuiteCommit) TestLongCommitMessageSerialization() {
	encoded := &plumbing.MemoryObject{}
	decoded := &Commit{}
	commit := *s.Commit

	longMessage := "my message: message\n\n" + strings.Repeat("test", 4096) + "\nOK"
	commit.Message = longMessage

	err := commit.Encode(encoded)
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
	s.Equal(longMessage, decoded.Message)
}

func (s *SuiteCommit) TestPGPSignatureSerialization() {
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
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
	s.Equal(pgpsignature, decoded.PGPSignature)

	// signature with extra empty line, it caused "index out of range" when
	// parsing it

	pgpsignature2 := "\n" + pgpsignature

	commit.PGPSignature = pgpsignature2
	encoded = &plumbing.MemoryObject{}
	decoded = &Commit{}

	err = commit.Encode(encoded)
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
	s.Equal(pgpsignature2, decoded.PGPSignature)

	// signature in author name

	commit.PGPSignature = ""
	commit.Author.Name = beginpgp
	encoded = &plumbing.MemoryObject{}
	decoded = &Commit{}

	err = commit.Encode(encoded)
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
	s.Equal("", decoded.PGPSignature)
	s.Equal(beginpgp, decoded.Author.Name)

	// broken signature

	commit.PGPSignature = beginpgp + "\n" +
		"some\n" +
		"trash\n" +
		endpgp +
		"text\n"
	encoded = &plumbing.MemoryObject{}
	decoded = &Commit{}

	err = commit.Encode(encoded)
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
	s.Equal(commit.PGPSignature, decoded.PGPSignature)
}

func (s *SuiteCommit) TestStat() {
	aCommit := s.commit(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))
	fileStats, err := aCommit.Stats()
	s.NoError(err)

	s.Equal("vendor/foo.go", fileStats[0].Name)
	s.Equal(7, fileStats[0].Addition)
	s.Equal(0, fileStats[0].Deletion)
	s.Equal(" vendor/foo.go | 7 +++++++\n", fileStats[0].String())

	// Stats for another commit.
	aCommit = s.commit(plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	fileStats, err = aCommit.Stats()
	s.NoError(err)

	s.Equal("go/example.go", fileStats[0].Name)
	s.Equal(142, fileStats[0].Addition)
	s.Equal(0, fileStats[0].Deletion)
	s.Equal(" go/example.go | 142 +++++++++++++++++++++++++++++++++++++++++++++++++++++\n", fileStats[0].String())

	s.Equal("php/crappy.php", fileStats[1].Name)
	s.Equal(259, fileStats[1].Addition)
	s.Equal(0, fileStats[1].Deletion)
	s.Equal(" php/crappy.php | 259 +++++++++++++++++++++++++++++++++++++++++++++++++++++\n", fileStats[1].String())
}

func (s *SuiteCommit) TestVerify() {
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
	s.NoError(err)

	_, ok := e.Identities["go-git test key"]
	s.True(ok)
}

func (s *SuiteCommit) TestPatchCancel() {
	from := s.commit(plumbing.NewHash("918c48b83bd081e863dbe1b80f8998f058cd8294"))
	to := s.commit(plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	patch, err := from.PatchContext(ctx, to)
	s.Nil(patch)
	s.ErrorContains(err, "operation canceled")
}

func (s *SuiteCommit) TestMalformedHeader() {
	encoded := &plumbing.MemoryObject{}
	decoded := &Commit{}
	commit := *s.Commit

	commit.PGPSignature = "\n"
	commit.Author.Name = "\n"
	commit.Author.Email = "\n"
	commit.Committer.Name = "\n"
	commit.Committer.Email = "\n"

	err := commit.Encode(encoded)
	s.NoError(err)

	err = decoded.Decode(encoded)
	s.NoError(err)
}

func (s *SuiteCommit) TestEncodeWithoutSignature() {
	// Similar to TestString since no signature
	encoded := &plumbing.MemoryObject{}
	err := s.Commit.EncodeWithoutSignature(encoded)
	s.NoError(err)
	er, err := encoded.Reader()
	s.NoError(err)
	payload, err := io.ReadAll(er)
	s.NoError(err)

	s.Equal(""+
		"tree eba74343e2f15d62adedfd8c883ee0262b5c8021\n"+
		"parent 35e85108805c84807bc66a02d91535e1e24b38b9\n"+
		"parent a5b8b09e2f8fcb0bb99d3ccb0958157b40890d69\n"+
		"author Máximo Cuadros Ortiz <mcuadros@gmail.com> 1427802494 +0200\n"+
		"committer Máximo Cuadros Ortiz <mcuadros@gmail.com> 1427802494 +0200\n"+
		"\n"+
		"Merge branch 'master' of github.com:tyba/git-fixture\n",
		string(payload))
}

func (s *SuiteCommit) TestEncodeWithoutSignatureJujutsu() {
	object := &plumbing.MemoryObject{}
	object.SetType(plumbing.CommitObject)
	object.Write([]byte(`tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904
author John Doe <john.doe@example.com> 1755280730 -0700
committer John Doe <john.doe@example.com> 1755280730 -0700
change-id wxmuynokkzxmuwxwvnnpnptoyuypknwv
gpgsig -----BEGIN PGP SIGNATURE-----
 
 iHUEABMIAB0WIQSZpnSpGKbQbDaLe5iiNQl48cTY5gUCaJ91XQAKCRCiNQl48cTY
 5vCYAP9Sf1yV9oUviRIxEA+4rsGIx0hI6kqFajJ/3TtBjyCTggD+PFnKOxdXeFL2
 GLwcCzFIsmQmkLxuLypsg+vueDSLpsM=
 =VucY
 -----END PGP SIGNATURE-----

initial commit

Change-Id: I6a6a696432d51cbff02d53234ccaca6b151afc34
`))

	commit, err := DecodeCommit(s.Storer, object)
	s.NoError(err)

	// Similar to TestString since no signature
	encoded := &plumbing.MemoryObject{}
	err = commit.EncodeWithoutSignature(encoded)
	s.NoError(err)
	er, err := encoded.Reader()
	s.NoError(err)
	payload, err := io.ReadAll(er)
	s.NoError(err)

	s.Equal(`tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904
author John Doe <john.doe@example.com> 1755280730 -0700
committer John Doe <john.doe@example.com> 1755280730 -0700
change-id wxmuynokkzxmuwxwvnnpnptoyuypknwv

initial commit

Change-Id: I6a6a696432d51cbff02d53234ccaca6b151afc34
`, string(payload))
}

func (s *SuiteCommit) TestEncodeExtraHeaders() {
	object := &plumbing.MemoryObject{}
	object.SetType(plumbing.CommitObject)
	object.Write([]byte(`tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904
author John Doe <john.doe@example.com> 1755280730 -0700
committer John Doe <john.doe@example.com> 1755280730 -0700
continuedheader to be
 continued
continuedheader to be
 continued
 on
 more than
 a single line
simpleflag
 value no key

initial commit
`))

	commit, err := DecodeCommit(s.Storer, object)
	s.NoError(err)

	s.Equal(commit.ExtraHeaders, []ExtraHeader{
		{
			Key:   "continuedheader",
			Value: "to be\ncontinued",
		},
		{
			Key:   "continuedheader",
			Value: "to be\ncontinued\non\nmore than\na single line",
		},
		{
			Key:   "simpleflag",
			Value: "",
		},
		{
			Key:   "",
			Value: "value no key",
		},
	})

	// Similar to TestString since no signature
	encoded := &plumbing.MemoryObject{}
	err = commit.EncodeWithoutSignature(encoded)
	s.NoError(err)
	er, err := encoded.Reader()
	s.NoError(err)
	payload, err := io.ReadAll(er)
	s.NoError(err)

	s.Equal(`tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904
author John Doe <john.doe@example.com> 1755280730 -0700
committer John Doe <john.doe@example.com> 1755280730 -0700
continuedheader to be
 continued
continuedheader to be
 continued
 on
 more than
 a single line
simpleflag
 value no key

initial commit
`, string(payload))
}

func (s *SuiteCommit) TestLess() {
	when1 := time.Now()
	when2 := when1.Add(time.Hour)

	hash1 := plumbing.NewHash("1669dce138d9b841a518c64b10914d88f5e488ea")
	hash2 := plumbing.NewHash("2669dce138d9b841a518c64b10914d88f5e488ea")

	commitLessTests := []struct {
		Committer1When, Committer2When time.Time
		Author1When, Author2When       time.Time
		Hash1, Hash2                   plumbing.Hash
		Exp                            bool
	}{
		{when1, when1, when1, when1, hash1, hash2, true},
		{when1, when1, when1, when1, hash2, hash1, false},
		{when1, when1, when1, when2, hash1, hash2, true},
		{when1, when1, when1, when2, hash2, hash1, true},
		{when1, when1, when2, when1, hash1, hash2, false},
		{when1, when1, when2, when1, hash2, hash1, false},
		{when1, when1, when2, when2, hash1, hash2, true},
		{when1, when1, when2, when2, hash2, hash1, false},
		{when1, when2, when1, when1, hash1, hash2, true},
		{when1, when2, when1, when1, hash2, hash1, true},
		{when1, when2, when1, when2, hash1, hash2, true},
		{when1, when2, when1, when2, hash2, hash1, true},
		{when1, when2, when2, when1, hash1, hash2, true},
		{when1, when2, when2, when1, hash2, hash1, true},
		{when1, when2, when2, when2, hash1, hash2, true},
		{when1, when2, when2, when2, hash2, hash1, true},
		{when2, when1, when1, when1, hash1, hash2, false},
		{when2, when1, when1, when1, hash2, hash1, false},
		{when2, when1, when1, when2, hash1, hash2, false},
		{when2, when1, when1, when2, hash2, hash1, false},
		{when2, when1, when2, when1, hash1, hash2, false},
		{when2, when1, when2, when1, hash2, hash1, false},
		{when2, when1, when2, when2, hash1, hash2, false},
		{when2, when1, when2, when2, hash2, hash1, false},
		{when2, when2, when1, when1, hash1, hash2, true},
		{when2, when2, when1, when1, hash2, hash1, false},
		{when2, when2, when1, when2, hash1, hash2, true},
		{when2, when2, when1, when2, hash2, hash1, true},
		{when2, when2, when2, when1, hash1, hash2, false},
		{when2, when2, when2, when1, hash2, hash1, false},
		{when2, when2, when2, when2, hash1, hash2, true},
		{when2, when2, when2, when2, hash2, hash1, false},
	}

	for _, t := range commitLessTests {
		commit1 := &Commit{
			Hash: t.Hash1,
			Author: Signature{
				When: t.Author1When,
			},
			Committer: Signature{
				When: t.Committer1When,
			},
		}
		commit2 := &Commit{
			Hash: t.Hash2,
			Author: Signature{
				When: t.Author2When,
			},
			Committer: Signature{
				When: t.Committer2When,
			},
		}
		s.Equal(t.Exp, commit1.Less(commit2))
	}
}
