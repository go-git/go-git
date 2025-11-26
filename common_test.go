package git

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
)

type BaseSuite struct {
	suite.Suite
	Repository *Repository

	cache map[string]*Repository
}

func (s *BaseSuite) SetupSuite() {
	s.buildBasicRepository()

	s.cache = make(map[string]*Repository)
}

func (s *BaseSuite) buildBasicRepository() {
	f := fixtures.Basic().One()
	s.Repository = s.NewRepository(f)
}

// NewRepository returns a new repository using the .git folder, if the fixture
// is tagged as worktree the filesystem from fixture is used, otherwise a new
// memfs filesystem is used as worktree.
func (s *BaseSuite) NewRepository(f *fixtures.Fixture) *Repository {
	dotgit := f.DotGit()
	worktree := memfs.New()
	if f.Is("worktree") {
		worktree = f.Worktree()
	}

	st := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	r, err := Open(st, worktree)
	s.Require().NoError(err)

	return r
}

// NewRepositoryWithEmptyWorktree returns a new repository using the .git folder
// from the fixture but without a empty memfs worktree, the index and the
// modules are deleted from the .git folder.
func NewRepositoryWithEmptyWorktree(f *fixtures.Fixture) *Repository {
	dotgit := f.DotGit()
	err := dotgit.Remove("index")
	if err != nil {
		panic(err)
	}

	err = util.RemoveAll(dotgit, "modules")
	if err != nil {
		panic(err)
	}

	worktree := memfs.New()

	st := filesystem.NewStorage(dotgit, cache.NewObjectLRUDefault())

	r, err := Open(st, worktree)
	if err != nil {
		panic(err)
	}

	return r
}

func (s *BaseSuite) NewRepositoryFromPackfile(f *fixtures.Fixture) *Repository {
	h := f.PackfileHash
	if r, ok := s.cache[h]; ok {
		return r
	}

	storer := memory.NewStorage()
	p := f.Packfile()
	defer func() { _ = p.Close() }()

	s.Require().NoError(packfile.UpdateObjectStorage(storer, p))
	s.Require().NoError(storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(f.Head))))

	r, err := Open(storer, memfs.New())
	s.Require().NoError(err)

	s.cache[h] = r
	return r
}

func (s *BaseSuite) GetBasicLocalRepositoryURL() string {
	fixture := fixtures.Basic().One()
	return s.GetLocalRepositoryURL(fixture)
}

func (s *BaseSuite) GetLocalRepositoryURL(f *fixtures.Fixture) string {
	return f.DotGit(fixtures.WithTargetDir(s.T().TempDir)).Root()
}

func (s *BaseSuite) TemporalHomeDir() (path string, clean func()) {
	home, err := os.UserHomeDir()
	s.Require().NoError(err)

	fs := osfs.New(home)
	relPath, err := util.TempDir(fs, "", "")
	s.Require().NoError(err)

	path = fs.Join(fs.Root(), relPath)
	clean = func() {
		_ = util.RemoveAll(fs, relPath)
	}

	return path, clean
}

func (s *BaseSuite) TemporalFilesystem() (fs billy.Filesystem) {
	fs = osfs.New(s.T().TempDir())
	path, err := util.TempDir(fs, "", "")
	s.Require().NoError(err)

	fs, err = fs.Chroot(path)
	s.Require().NoError(err)

	return fs
}

type SuiteCommon struct {
	suite.Suite
}

func TestSuiteCommon(t *testing.T) {
	suite.Run(t, new(SuiteCommon))
}

var countLinesTests = [...]struct {
	i string // the string we want to count lines from
	e int    // the expected number of lines in i
}{
	{"", 0},
	{"a", 1},
	{"a\n", 1},
	{"a\nb", 2},
	{"a\nb\n", 2},
	{"a\nb\nc", 3},
	{"a\nb\nc\n", 3},
	{"a\n\n\nb\n", 4},
	{"first line\n\tsecond line\nthird line\n", 3},
}

func (s *SuiteCommon) TestCountLines() {
	for i, t := range countLinesTests {
		o := countLines(t.i)
		s.Equal(t.e, o, fmt.Sprintf("subtest %d, input=%q", i, t.i))
	}
}

func AssertReferences(t *testing.T, r *Repository, expected map[string]string) {
	for name, target := range expected {
		expected := plumbing.NewReferenceFromStrings(name, target)

		obtained, err := r.Reference(expected.Name(), true)
		assert.NoError(t, err)

		assert.Equal(t, expected, obtained)
	}
}

func AssertReferencesMissing(t *testing.T, r *Repository, expected []string) {
	for _, name := range expected {
		_, err := r.Reference(plumbing.ReferenceName(name), false)
		assert.Error(t, err)
		assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
	}
}

func CommitNewFile(t *testing.T, repo *Repository, fileName string) plumbing.Hash {
	wt, err := repo.Worktree()
	assert.NoError(t, err)

	fd, err := wt.Filesystem.Create(fileName)
	assert.NoError(t, err)

	_, err = fd.Write([]byte("# test file"))
	assert.NoError(t, err)

	err = fd.Close()
	assert.NoError(t, err)

	_, err = wt.Add(fileName)
	assert.NoError(t, err)

	sha, err := wt.Commit("test commit", &CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@example.com",
			When:  time.Now(),
		},
		Committer: &object.Signature{
			Name:  "test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	assert.NoError(t, err)

	return sha
}
