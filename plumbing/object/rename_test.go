package object

import (
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/storage/memory"
	. "gopkg.in/check.v1"
)

type RenameSuite struct {
	BaseObjectsSuite
}

var _ = Suite(&RenameSuite{})

func (s *RenameSuite) TestNameSimilarityScore(c *C) {
	testCases := []struct {
		a, b  string
		score int
	}{
		{"foo/bar.c", "foo/baz.c", 70},
		{"src/utils/Foo.java", "tests/utils/Foo.java", 64},
		{"foo/bar/baz.py", "README.md", 0},
		{"src/utils/something/foo.py", "src/utils/something/other/foo.py", 69},
		{"src/utils/something/foo.py", "src/utils/yada/foo.py", 63},
		{"src/utils/something/foo.py", "src/utils/something/other/bar.py", 44},
		{"src/utils/something/foo.py", "src/utils/something/foo.py", 100},
	}

	for _, tt := range testCases {
		c.Assert(nameSimilarityScore(tt.a, tt.b), Equals, tt.score)
	}
}

const (
	pathA = "src/A"
	pathB = "src/B"
	pathH = "src/H"
	pathQ = "src/Q"
)

func (s *RenameSuite) TestExactRename_OneRename(c *C) {
	a := makeAdd(c, makeFile(c, pathA, filemode.Regular, "foo"))
	b := makeDelete(c, makeFile(c, pathQ, filemode.Regular, "foo"))

	result := detectRenames(c, Changes{a, b}, nil, 1)
	assertRename(c, b, a, result[0])
}

func (s *RenameSuite) TestExactRename_DifferentObjects(c *C) {
	a := makeAdd(c, makeFile(c, pathA, filemode.Regular, "foo"))
	h := makeAdd(c, makeFile(c, pathH, filemode.Regular, "foo"))
	q := makeDelete(c, makeFile(c, pathQ, filemode.Regular, "bar"))

	result := detectRenames(c, Changes{a, h, q}, nil, 3)
	c.Assert(result[0], DeepEquals, a)
	c.Assert(result[1], DeepEquals, h)
	c.Assert(result[2], DeepEquals, q)
}

func (s *RenameSuite) TestExactRename_OneRenameOneModify(c *C) {
	c1 := makeAdd(c, makeFile(c, pathA, filemode.Regular, "foo"))
	c2 := makeDelete(c, makeFile(c, pathQ, filemode.Regular, "foo"))
	c3 := makeChange(c,
		makeFile(c, pathH, filemode.Regular, "bar"),
		makeFile(c, pathH, filemode.Regular, "bar"),
	)

	result := detectRenames(c, Changes{c1, c2, c3}, nil, 2)
	c.Assert(result[0], DeepEquals, c3)
	assertRename(c, c2, c1, result[1])
}

func (s *RenameSuite) TestExactRename_ManyRenames(c *C) {
	c1 := makeAdd(c, makeFile(c, pathA, filemode.Regular, "foo"))
	c2 := makeDelete(c, makeFile(c, pathQ, filemode.Regular, "foo"))
	c3 := makeAdd(c, makeFile(c, pathH, filemode.Regular, "bar"))
	c4 := makeDelete(c, makeFile(c, pathB, filemode.Regular, "bar"))

	result := detectRenames(c, Changes{c1, c2, c3, c4}, nil, 2)
	assertRename(c, c4, c3, result[0])
	assertRename(c, c2, c1, result[1])
}

func (s *RenameSuite) TestExactRename_MultipleIdenticalDeletes(c *C) {
	changes := Changes{
		makeDelete(c, makeFile(c, pathA, filemode.Regular, "foo")),
		makeDelete(c, makeFile(c, pathB, filemode.Regular, "foo")),
		makeDelete(c, makeFile(c, pathH, filemode.Regular, "foo")),
		makeAdd(c, makeFile(c, pathQ, filemode.Regular, "foo")),
	}

	result := detectRenames(c, changes, nil, 3)
	assertRename(c, changes[0], changes[3], result[0])
	c.Assert(result[1], DeepEquals, changes[1])
	c.Assert(result[2], DeepEquals, changes[2])
}

func (s *RenameSuite) TestRenameExact_PathBreaksTie(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, "src/com/foo/a.java", filemode.Regular, "foo")),
		makeDelete(c, makeFile(c, "src/com/foo/b.java", filemode.Regular, "foo")),
		makeAdd(c, makeFile(c, "c.txt", filemode.Regular, "foo")),
		makeDelete(c, makeFile(c, "d.txt", filemode.Regular, "foo")),
		makeAdd(c, makeFile(c, "the_e_file.txt", filemode.Regular, "foo")),
	}

	// Add out of order to avoid first-match succeeding
	result := detectRenames(c, Changes{
		changes[0],
		changes[3],
		changes[4],
		changes[1],
		changes[2],
	}, nil, 3)
	assertRename(c, changes[3], changes[2], result[0])
	assertRename(c, changes[1], changes[0], result[1])
	c.Assert(result[2], DeepEquals, changes[4])
}

func (s *RenameSuite) TestExactRename_OneDeleteManyAdds(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, "src/com/foo/a.java", filemode.Regular, "foo")),
		makeAdd(c, makeFile(c, "src/com/foo/b.java", filemode.Regular, "foo")),
		makeAdd(c, makeFile(c, "c.txt", filemode.Regular, "foo")),
		makeDelete(c, makeFile(c, "d.txt", filemode.Regular, "foo")),
	}

	result := detectRenames(c, changes, nil, 3)
	assertRename(c, changes[3], changes[2], result[0])
	c.Assert(result[1], DeepEquals, changes[0])
	c.Assert(result[2], DeepEquals, changes[1])
}

func (s *RenameSuite) TestExactRename_UnstagedFile(c *C) {
	changes := Changes{
		makeDelete(c, makeFile(c, pathA, filemode.Regular, "foo")),
		makeAdd(c, makeFile(c, pathB, filemode.Regular, "foo")),
	}
	result := detectRenames(c, changes, nil, 1)
	assertRename(c, changes[0], changes[1], result[0])
}

func (s *RenameSuite) TestContentRename_OnePair(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "foo\nbar\nbaz\nblarg\n")),
		makeDelete(c, makeFile(c, pathA, filemode.Regular, "foo\nbar\nbaz\nblah\n")),
	}

	result := detectRenames(c, changes, nil, 1)
	assertRename(c, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestContentRename_OneRenameTwoUnrelatedFiles(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "foo\nbar\nbaz\nblarg\n")),
		makeDelete(c, makeFile(c, pathQ, filemode.Regular, "foo\nbar\nbaz\nblah\n")),
		makeAdd(c, makeFile(c, pathB, filemode.Regular, "some\nsort\nof\ntext\n")),
		makeDelete(c, makeFile(c, pathH, filemode.Regular, "completely\nunrelated\ntext\n")),
	}

	result := detectRenames(c, changes, nil, 3)
	c.Assert(result[0], DeepEquals, changes[2])
	c.Assert(result[1], DeepEquals, changes[3])
	assertRename(c, changes[1], changes[0], result[2])
}

func (s *RenameSuite) TestContentRename_LastByteDifferent(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "foo\nbar\na")),
		makeDelete(c, makeFile(c, pathQ, filemode.Regular, "foo\nbar\nb")),
	}

	result := detectRenames(c, changes, nil, 1)
	assertRename(c, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestContentRename_NewlinesOnly(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, strings.Repeat("\n", 3))),
		makeDelete(c, makeFile(c, pathQ, filemode.Regular, strings.Repeat("\n", 4))),
	}

	result := detectRenames(c, changes, nil, 1)
	assertRename(c, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestContentRename_SameContentMultipleTimes(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "a\na\na\na\n")),
		makeDelete(c, makeFile(c, pathQ, filemode.Regular, "a\na\na\n")),
	}

	result := detectRenames(c, changes, nil, 1)
	assertRename(c, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestContentRename_OnePairRenameScore50(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "ab\nab\nab\nac\nad\nae\n")),
		makeDelete(c, makeFile(c, pathQ, filemode.Regular, "ac\nab\nab\nab\naa\na0\na1\n")),
	}

	result := detectRenames(c, changes, &DiffTreeOptions{RenameScore: 50}, 1)
	assertRename(c, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestNoRenames_SingleByteFiles(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "a")),
		makeAdd(c, makeFile(c, pathQ, filemode.Regular, "b")),
	}

	result := detectRenames(c, changes, nil, 2)
	c.Assert(result[0], DeepEquals, changes[0])
	c.Assert(result[1], DeepEquals, changes[1])
}

func (s *RenameSuite) TestNoRenames_EmptyFile(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "")),
	}
	result := detectRenames(c, changes, nil, 1)
	c.Assert(result[0], DeepEquals, changes[0])
}

func (s *RenameSuite) TestNoRenames_EmptyFile2(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "")),
		makeDelete(c, makeFile(c, pathQ, filemode.Regular, "blah")),
	}
	result := detectRenames(c, changes, nil, 2)
	c.Assert(result[0], DeepEquals, changes[0])
	c.Assert(result[1], DeepEquals, changes[1])
}

func (s *RenameSuite) TestNoRenames_SymlinkAndFile(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "src/dest")),
		makeDelete(c, makeFile(c, pathQ, filemode.Symlink, "src/dest")),
	}
	result := detectRenames(c, changes, nil, 2)
	c.Assert(result[0], DeepEquals, changes[0])
	c.Assert(result[1], DeepEquals, changes[1])
}

func (s *RenameSuite) TestNoRenames_SymlinkAndFileSamePath(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "src/dest")),
		makeDelete(c, makeFile(c, pathA, filemode.Symlink, "src/dest")),
	}
	result := detectRenames(c, changes, nil, 2)
	c.Assert(result[0], DeepEquals, changes[0])
	c.Assert(result[1], DeepEquals, changes[1])
}

func (s *RenameSuite) TestRenameLimit(c *C) {
	changes := Changes{
		makeAdd(c, makeFile(c, pathA, filemode.Regular, "foo\nbar\nbaz\nblarg\n")),
		makeDelete(c, makeFile(c, pathB, filemode.Regular, "foo\nbar\nbaz\nblah\n")),
		makeAdd(c, makeFile(c, pathH, filemode.Regular, "a\nb\nc\nd\n")),
		makeDelete(c, makeFile(c, pathQ, filemode.Regular, "a\nb\nc\n")),
	}

	result := detectRenames(c, changes, &DiffTreeOptions{RenameLimit: 1}, 4)
	for i, res := range result {
		c.Assert(res, DeepEquals, changes[i])
	}
}

func (s *RenameSuite) TestRenameExactManyAddsManyDeletesNoGaps(c *C) {
	content := "a"
	detector := &renameDetector{
		added: []*Change{
			makeAdd(c, makeFile(c, pathA, filemode.Regular, content)),
			makeAdd(c, makeFile(c, pathQ, filemode.Regular, content)),
			makeAdd(c, makeFile(c, "something", filemode.Regular, content)),
		},
		deleted: []*Change{
			makeDelete(c, makeFile(c, pathA, filemode.Regular, content)),
			makeDelete(c, makeFile(c, pathB, filemode.Regular, content)),
			makeDelete(c, makeFile(c, "foo/bar/other", filemode.Regular, content)),
		},
	}

	detector.detectExactRenames()

	for _, added := range detector.added {
		c.Assert(added, NotNil)
	}

	for _, deleted := range detector.deleted {
		c.Assert(deleted, NotNil)
	}
}

func detectRenames(c *C, changes Changes, opts *DiffTreeOptions, expectedResults int) Changes {
	result, err := DetectRenames(changes, opts)
	c.Assert(err, IsNil)
	c.Assert(result, HasLen, expectedResults)
	return result
}

func assertRename(c *C, from, to *Change, rename *Change) {
	c.Assert(&Change{From: from.From, To: to.To}, DeepEquals, rename)
}

type SimilarityIndexSuite struct {
	BaseObjectsSuite
}

var _ = Suite(&SimilarityIndexSuite{})

func (s *SimilarityIndexSuite) TestScoreFiles(c *C) {
	tree := s.tree(c, plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))
	binary, err := tree.File("binary.jpg")
	c.Assert(err, IsNil)
	binIndex, err := fileSimilarityIndex(binary)
	c.Assert(err, IsNil)

	long, err := tree.File("json/long.json")
	c.Assert(err, IsNil)
	longIndex, err := fileSimilarityIndex(long)
	c.Assert(err, IsNil)

	short, err := tree.File("json/short.json")
	c.Assert(err, IsNil)
	shortIndex, err := fileSimilarityIndex(short)
	c.Assert(err, IsNil)

	php, err := tree.File("php/crappy.php")
	c.Assert(err, IsNil)
	phpIndex, err := fileSimilarityIndex(php)
	c.Assert(err, IsNil)

	testCases := []struct {
		src, dst      *similarityIndex
		expectedScore int
	}{
		{binIndex, binIndex, 10000}, // same file
		{shortIndex, longIndex, 32}, // slightly similar files
		{longIndex, shortIndex, 32}, // same as previous, diff order
		{shortIndex, phpIndex, 1},   // different files
		{longIndex, binIndex, 0},    // code vs binary file
	}

	for _, tt := range testCases {
		score := tt.src.score(tt.dst, 10000)
		c.Assert(score, Equals, tt.expectedScore)
	}
}

func (s *SimilarityIndexSuite) TestHashContent(c *C) {
	idx := textIndex(c, "A\n"+
		"B\n"+
		"D\n"+
		"B\n")

	keyA := keyFor(c, "A\n")
	keyB := keyFor(c, "B\n")
	keyD := keyFor(c, "D\n")

	c.Assert(keyA, Not(Equals), keyB)
	c.Assert(keyA, Not(Equals), keyD)
	c.Assert(keyD, Not(Equals), keyB)

	c.Assert(idx.numHashes, Equals, 3)
	c.Assert(idx.hashes[findIndex(idx, keyA)].count(), Equals, uint64(2))
	c.Assert(idx.hashes[findIndex(idx, keyB)].count(), Equals, uint64(4))
	c.Assert(idx.hashes[findIndex(idx, keyD)].count(), Equals, uint64(2))
}

func (s *SimilarityIndexSuite) TestCommonSameFiles(c *C) {
	content := "A\n" +
		"B\n" +
		"D\n" +
		"B\n"

	src := textIndex(c, content)
	dst := textIndex(c, content)

	c.Assert(src.common(dst), Equals, uint64(8))
	c.Assert(dst.common(src), Equals, uint64(8))

	c.Assert(src.score(dst, 100), Equals, 100)
	c.Assert(dst.score(src, 100), Equals, 100)
}

func (s *SimilarityIndexSuite) TestCommonSameFilesCR(c *C) {
	content := "A\r\n" +
		"B\r\n" +
		"D\r\n" +
		"B\r\n"

	src := textIndex(c, content)
	dst := textIndex(c, strings.ReplaceAll(content, "\r", ""))

	c.Assert(src.common(dst), Equals, uint64(8))
	c.Assert(dst.common(src), Equals, uint64(8))

	c.Assert(src.score(dst, 100), Equals, 100)
	c.Assert(dst.score(src, 100), Equals, 100)
}

func (s *SimilarityIndexSuite) TestCommonEmptyFiles(c *C) {
	src := textIndex(c, "")
	dst := textIndex(c, "")

	c.Assert(src.common(dst), Equals, uint64(0))
	c.Assert(dst.common(src), Equals, uint64(0))
}

func (s *SimilarityIndexSuite) TestCommonTotallyDifferentFiles(c *C) {
	src := textIndex(c, "A\n")
	dst := textIndex(c, "D\n")

	c.Assert(src.common(dst), Equals, uint64(0))
	c.Assert(dst.common(src), Equals, uint64(0))
}

func (s *SimilarityIndexSuite) TestSimilarity75(c *C) {
	src := textIndex(c, "A\nB\nC\nD\n")
	dst := textIndex(c, "A\nB\nC\nQ\n")

	c.Assert(src.common(dst), Equals, uint64(6))
	c.Assert(dst.common(src), Equals, uint64(6))

	c.Assert(src.score(dst, 100), Equals, 75)
	c.Assert(dst.score(src, 100), Equals, 75)
}

func keyFor(c *C, line string) int {
	idx := newSimilarityIndex()
	err := idx.hashContent(strings.NewReader(line), int64(len(line)), false)
	c.Assert(err, IsNil)

	c.Assert(idx.numHashes, Equals, 1)
	for _, h := range idx.hashes {
		if h != 0 {
			return h.key()
		}
	}

	return -1
}

func textIndex(c *C, content string) *similarityIndex {
	idx := newSimilarityIndex()
	err := idx.hashContent(strings.NewReader(content), int64(len(content)), false)
	c.Assert(err, IsNil)
	return idx
}

func findIndex(idx *similarityIndex, key int) int {
	for i, h := range idx.hashes {
		if h.key() == key {
			return i
		}
	}
	return -1
}

func makeFile(c *C, name string, mode filemode.FileMode, content string) *File {
	obj := new(plumbing.MemoryObject)
	obj.SetType(plumbing.BlobObject)
	_, err := obj.Write([]byte(content))
	c.Assert(err, IsNil)
	return &File{
		Name: name,
		Mode: mode,
		Blob: Blob{Hash: obj.Hash(), Size: obj.Size(), obj: obj},
	}
}

func makeChangeEntry(f *File) ChangeEntry {
	sto := memory.NewStorage()
	sto.SetEncodedObject(f.obj)
	tree := &Tree{s: sto}

	return ChangeEntry{
		Name: f.Name,
		Tree: tree,
		TreeEntry: TreeEntry{
			Name: filepath.Base(f.Name),
			Mode: f.Mode,
			Hash: f.Hash,
		},
	}
}

func makeAdd(c *C, f *File) *Change {
	return makeChange(c, nil, f)
}

func makeDelete(c *C, f *File) *Change {
	return makeChange(c, f, nil)
}

func makeChange(c *C, from *File, to *File) *Change {
	if from == nil {
		return &Change{To: makeChangeEntry(to)}
	}

	if to == nil {
		return &Change{From: makeChangeEntry(from)}
	}

	if from == nil && to == nil {
		c.Error("cannot make change without from or to")
	}

	return &Change{From: makeChangeEntry(from), To: makeChangeEntry(to)}
}
