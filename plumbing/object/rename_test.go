package object

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/storage/memory"
)

type RenameSuite struct {
	suite.Suite
	BaseObjectsSuite
}

func TestRenameSuite(t *testing.T) {
	suite.Run(t, new(RenameSuite))
}

func (s *RenameSuite) TestNameSimilarityScore() {
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
		s.Equal(tt.score, nameSimilarityScore(tt.a, tt.b))
	}
}

const (
	pathA = "src/A"
	pathB = "src/B"
	pathH = "src/H"
	pathQ = "src/Q"
)

func (s *RenameSuite) TestExactRename_OneRename() {
	a := makeAdd(s, makeFile(s, pathA, filemode.Regular, "foo"))
	b := makeDelete(s, makeFile(s, pathQ, filemode.Regular, "foo"))

	result := detectRenames(s, Changes{a, b}, nil, 1)
	assertRename(s, b, a, result[0])
}

func (s *RenameSuite) TestExactRename_DifferentObjects() {
	a := makeAdd(s, makeFile(s, pathA, filemode.Regular, "foo"))
	h := makeAdd(s, makeFile(s, pathH, filemode.Regular, "foo"))
	q := makeDelete(s, makeFile(s, pathQ, filemode.Regular, "bar"))

	result := detectRenames(s, Changes{a, h, q}, nil, 3)
	s.Equal(a, result[0])
	s.Equal(h, result[1])
	s.Equal(q, result[2])
}

func (s *RenameSuite) TestExactRename_OneRenameOneModify() {
	c1 := makeAdd(s, makeFile(s, pathA, filemode.Regular, "foo"))
	c2 := makeDelete(s, makeFile(s, pathQ, filemode.Regular, "foo"))
	c3 := makeChange(s,
		makeFile(s, pathH, filemode.Regular, "bar"),
		makeFile(s, pathH, filemode.Regular, "bar"),
	)

	result := detectRenames(s, Changes{c1, c2, c3}, nil, 2)
	s.Equal(c3, result[0])
	assertRename(s, c2, c1, result[1])
}

func (s *RenameSuite) TestExactRename_ManyRenames() {
	c1 := makeAdd(s, makeFile(s, pathA, filemode.Regular, "foo"))
	c2 := makeDelete(s, makeFile(s, pathQ, filemode.Regular, "foo"))
	c3 := makeAdd(s, makeFile(s, pathH, filemode.Regular, "bar"))
	c4 := makeDelete(s, makeFile(s, pathB, filemode.Regular, "bar"))

	result := detectRenames(s, Changes{c1, c2, c3, c4}, nil, 2)
	assertRename(s, c4, c3, result[0])
	assertRename(s, c2, c1, result[1])
}

func (s *RenameSuite) TestExactRename_MultipleIdenticalDeletes() {
	changes := Changes{
		makeDelete(s, makeFile(s, pathA, filemode.Regular, "foo")),
		makeDelete(s, makeFile(s, pathB, filemode.Regular, "foo")),
		makeDelete(s, makeFile(s, pathH, filemode.Regular, "foo")),
		makeAdd(s, makeFile(s, pathQ, filemode.Regular, "foo")),
	}

	result := detectRenames(s, changes, nil, 3)
	assertRename(s, changes[0], changes[3], result[0])
	s.Equal(changes[1], result[1])
	s.Equal(changes[2], result[2])
}

func (s *RenameSuite) TestRenameExact_PathBreaksTie() {
	changes := Changes{
		makeAdd(s, makeFile(s, "src/com/foo/a.java", filemode.Regular, "foo")),
		makeDelete(s, makeFile(s, "src/com/foo/b.java", filemode.Regular, "foo")),
		makeAdd(s, makeFile(s, "c.txt", filemode.Regular, "foo")),
		makeDelete(s, makeFile(s, "d.txt", filemode.Regular, "foo")),
		makeAdd(s, makeFile(s, "the_e_file.txt", filemode.Regular, "foo")),
	}

	// Add out of order to avoid first-match succeeding
	result := detectRenames(s, Changes{
		changes[0],
		changes[3],
		changes[4],
		changes[1],
		changes[2],
	}, nil, 3)
	assertRename(s, changes[3], changes[2], result[0])
	assertRename(s, changes[1], changes[0], result[1])
	s.Equal(changes[4], result[2])
}

func (s *RenameSuite) TestExactRename_OneDeleteManyAdds() {
	changes := Changes{
		makeAdd(s, makeFile(s, "src/com/foo/a.java", filemode.Regular, "foo")),
		makeAdd(s, makeFile(s, "src/com/foo/b.java", filemode.Regular, "foo")),
		makeAdd(s, makeFile(s, "c.txt", filemode.Regular, "foo")),
		makeDelete(s, makeFile(s, "d.txt", filemode.Regular, "foo")),
	}

	result := detectRenames(s, changes, nil, 3)
	assertRename(s, changes[3], changes[2], result[0])
	s.Equal(changes[0], result[1])
	s.Equal(changes[1], result[2])
}

func (s *RenameSuite) TestExactRename_UnstagedFile() {
	changes := Changes{
		makeDelete(s, makeFile(s, pathA, filemode.Regular, "foo")),
		makeAdd(s, makeFile(s, pathB, filemode.Regular, "foo")),
	}
	result := detectRenames(s, changes, nil, 1)
	assertRename(s, changes[0], changes[1], result[0])
}

func (s *RenameSuite) TestContentRename_OnePair() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "foo\nbar\nbaz\nblarg\n")),
		makeDelete(s, makeFile(s, pathA, filemode.Regular, "foo\nbar\nbaz\nblah\n")),
	}

	result := detectRenames(s, changes, nil, 1)
	assertRename(s, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestContentRename_OneRenameTwoUnrelatedFiles() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "foo\nbar\nbaz\nblarg\n")),
		makeDelete(s, makeFile(s, pathQ, filemode.Regular, "foo\nbar\nbaz\nblah\n")),
		makeAdd(s, makeFile(s, pathB, filemode.Regular, "some\nsort\nof\ntext\n")),
		makeDelete(s, makeFile(s, pathH, filemode.Regular, "completely\nunrelated\ntext\n")),
	}

	result := detectRenames(s, changes, nil, 3)
	s.Equal(changes[2], result[0])
	s.Equal(changes[3], result[1])
	assertRename(s, changes[1], changes[0], result[2])
}

func (s *RenameSuite) TestContentRename_LastByteDifferent() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "foo\nbar\na")),
		makeDelete(s, makeFile(s, pathQ, filemode.Regular, "foo\nbar\nb")),
	}

	result := detectRenames(s, changes, nil, 1)
	assertRename(s, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestContentRename_NewlinesOnly() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, strings.Repeat("\n", 3))),
		makeDelete(s, makeFile(s, pathQ, filemode.Regular, strings.Repeat("\n", 4))),
	}

	result := detectRenames(s, changes, nil, 1)
	assertRename(s, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestContentRename_SameContentMultipleTimes() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "a\na\na\na\n")),
		makeDelete(s, makeFile(s, pathQ, filemode.Regular, "a\na\na\n")),
	}

	result := detectRenames(s, changes, nil, 1)
	assertRename(s, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestContentRename_OnePairRenameScore50() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "ab\nab\nab\nac\nad\nae\n")),
		makeDelete(s, makeFile(s, pathQ, filemode.Regular, "ac\nab\nab\nab\naa\na0\na1\n")),
	}

	result := detectRenames(s, changes, &DiffTreeOptions{RenameScore: 50}, 1)
	assertRename(s, changes[1], changes[0], result[0])
}

func (s *RenameSuite) TestNoRenames_SingleByteFiles() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "a")),
		makeAdd(s, makeFile(s, pathQ, filemode.Regular, "b")),
	}

	result := detectRenames(s, changes, nil, 2)
	s.Equal(changes[0], result[0])
	s.Equal(changes[1], result[1])
}

func (s *RenameSuite) TestNoRenames_EmptyFile() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "")),
	}
	result := detectRenames(s, changes, nil, 1)
	s.Equal(changes[0], result[0])
}

func (s *RenameSuite) TestNoRenames_EmptyFile2() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "")),
		makeDelete(s, makeFile(s, pathQ, filemode.Regular, "blah")),
	}
	result := detectRenames(s, changes, nil, 2)
	s.Equal(changes[0], result[0])
	s.Equal(changes[1], result[1])
}

func (s *RenameSuite) TestNoRenames_SymlinkAndFile() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "src/dest")),
		makeDelete(s, makeFile(s, pathQ, filemode.Symlink, "src/dest")),
	}
	result := detectRenames(s, changes, nil, 2)
	s.Equal(changes[0], result[0])
	s.Equal(changes[1], result[1])
}

func (s *RenameSuite) TestNoRenames_SymlinkAndFileSamePath() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "src/dest")),
		makeDelete(s, makeFile(s, pathA, filemode.Symlink, "src/dest")),
	}
	result := detectRenames(s, changes, nil, 2)
	s.Equal(changes[0], result[0])
	s.Equal(changes[1], result[1])
}

func (s *RenameSuite) TestRenameLimit() {
	changes := Changes{
		makeAdd(s, makeFile(s, pathA, filemode.Regular, "foo\nbar\nbaz\nblarg\n")),
		makeDelete(s, makeFile(s, pathB, filemode.Regular, "foo\nbar\nbaz\nblah\n")),
		makeAdd(s, makeFile(s, pathH, filemode.Regular, "a\nb\nc\nd\n")),
		makeDelete(s, makeFile(s, pathQ, filemode.Regular, "a\nb\nc\n")),
	}

	result := detectRenames(s, changes, &DiffTreeOptions{RenameLimit: 1}, 4)
	for i, res := range result {
		s.Equal(changes[i], res)
	}
}

func (s *RenameSuite) TestRenameExactManyAddsManyDeletesNoGaps() {
	content := "a"
	detector := &renameDetector{
		added: []*Change{
			makeAdd(s, makeFile(s, pathA, filemode.Regular, content)),
			makeAdd(s, makeFile(s, pathQ, filemode.Regular, content)),
			makeAdd(s, makeFile(s, "something", filemode.Regular, content)),
		},
		deleted: []*Change{
			makeDelete(s, makeFile(s, pathA, filemode.Regular, content)),
			makeDelete(s, makeFile(s, pathB, filemode.Regular, content)),
			makeDelete(s, makeFile(s, "foo/bar/other", filemode.Regular, content)),
		},
	}

	detector.detectExactRenames()

	for _, added := range detector.added {
		s.NotNil(added)
	}

	for _, deleted := range detector.deleted {
		s.NotNil(deleted)
	}
}

func detectRenames(s *RenameSuite, changes Changes, opts *DiffTreeOptions, expectedResults int) Changes {
	result, err := DetectRenames(changes, opts)
	s.NoError(err)
	s.Len(result, expectedResults)
	return result
}

func assertRename(s *RenameSuite, from, to, rename *Change) {
	s.Equal(rename, &Change{From: from.From, To: to.To})
}

type SimilarityIndexSuite struct {
	suite.Suite
	BaseObjectsSuite
}

func TestSimilarityIndexSuite(t *testing.T) {
	suite.Run(t, new(SimilarityIndexSuite))
}

func (s *SimilarityIndexSuite) SetupSuite() {
	s.BaseObjectsSuite.SetupSuite(s.T())
}

func (s *SimilarityIndexSuite) TestScoreFiles() {
	tree := s.tree(plumbing.NewHash("a8d315b2b1c615d43042c3a62402b8a54288cf5c"))
	binary, err := tree.File("binary.jpg")
	s.NoError(err)
	binIndex, err := fileSimilarityIndex(binary)
	s.NoError(err)

	long, err := tree.File("json/long.json")
	s.NoError(err)
	longIndex, err := fileSimilarityIndex(long)
	s.NoError(err)

	short, err := tree.File("json/short.json")
	s.NoError(err)
	shortIndex, err := fileSimilarityIndex(short)
	s.NoError(err)

	php, err := tree.File("php/crappy.php")
	s.NoError(err)
	phpIndex, err := fileSimilarityIndex(php)
	s.NoError(err)

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
		s.Equal(tt.expectedScore, score)
	}
}

func (s *SimilarityIndexSuite) TestHashContent() {
	idx := textIndex(s, "A\n"+
		"B\n"+
		"D\n"+
		"B\n")

	keyA := keyFor(s, "A\n")
	keyB := keyFor(s, "B\n")
	keyD := keyFor(s, "D\n")

	s.NotEqual(keyB, keyA)
	s.NotEqual(keyD, keyA)
	s.NotEqual(keyB, keyD)

	s.Equal(3, idx.numHashes)
	s.Equal(uint64(2), idx.hashes[findIndex(idx, keyA)].count())
	s.Equal(uint64(4), idx.hashes[findIndex(idx, keyB)].count())
	s.Equal(uint64(2), idx.hashes[findIndex(idx, keyD)].count())
}

func (s *SimilarityIndexSuite) TestCommonSameFiles() {
	content := "A\n" +
		"B\n" +
		"D\n" +
		"B\n"

	src := textIndex(s, content)
	dst := textIndex(s, content)

	s.Equal(uint64(8), src.common(dst))
	s.Equal(uint64(8), dst.common(src))

	s.Equal(100, src.score(dst, 100))
	s.Equal(100, dst.score(src, 100))
}

func (s *SimilarityIndexSuite) TestCommonSameFilesCR() {
	content := "A\r\n" +
		"B\r\n" +
		"D\r\n" +
		"B\r\n"

	src := textIndex(s, content)
	dst := textIndex(s, strings.ReplaceAll(content, "\r", ""))

	s.Equal(uint64(8), src.common(dst))
	s.Equal(uint64(8), dst.common(src))

	s.Equal(100, src.score(dst, 100))
	s.Equal(100, dst.score(src, 100))
}

func (s *SimilarityIndexSuite) TestCommonEmptyFiles() {
	src := textIndex(s, "")
	dst := textIndex(s, "")

	s.Equal(uint64(0), src.common(dst))
	s.Equal(uint64(0), dst.common(src))
}

func (s *SimilarityIndexSuite) TestCommonTotallyDifferentFiles() {
	src := textIndex(s, "A\n")
	dst := textIndex(s, "D\n")

	s.Equal(uint64(0), src.common(dst))
	s.Equal(uint64(0), dst.common(src))
}

func (s *SimilarityIndexSuite) TestSimilarity75() {
	src := textIndex(s, "A\nB\nC\nD\n")
	dst := textIndex(s, "A\nB\nC\nQ\n")

	s.Equal(uint64(6), src.common(dst))
	s.Equal(uint64(6), dst.common(src))

	s.Equal(75, src.score(dst, 100))
	s.Equal(75, dst.score(src, 100))
}

func keyFor(s *SimilarityIndexSuite, line string) int {
	idx := newSimilarityIndex()
	err := idx.hashContent(strings.NewReader(line), int64(len(line)), false)
	s.NoError(err)

	s.Equal(1, idx.numHashes)
	for _, h := range idx.hashes {
		if h != 0 {
			return h.key()
		}
	}

	return -1
}

func textIndex(s *SimilarityIndexSuite, content string) *similarityIndex {
	idx := newSimilarityIndex()
	err := idx.hashContent(strings.NewReader(content), int64(len(content)), false)
	s.NoError(err)
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

func makeFile(s *RenameSuite, name string, mode filemode.FileMode, content string) *File {
	obj := new(plumbing.MemoryObject)
	obj.SetType(plumbing.BlobObject)
	_, err := obj.Write([]byte(content))
	s.NoError(err)
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

func makeAdd(s *RenameSuite, f *File) *Change {
	return makeChange(s, nil, f)
}

func makeDelete(s *RenameSuite, f *File) *Change {
	return makeChange(s, f, nil)
}

func makeChange(s *RenameSuite, from, to *File) *Change {
	if from == nil {
		return &Change{To: makeChangeEntry(to)}
	}

	if to == nil {
		return &Change{From: makeChangeEntry(from)}
	}

	if from == nil && to == nil {
		s.Fail("cannot make change without from or to")
	}

	return &Change{From: makeChangeEntry(from), To: makeChangeEntry(to)}
}
