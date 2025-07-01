package gitattributes

import (
	"os"
	"strconv"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/suite"
)

type MatcherSuite struct {
	suite.Suite
	GFS  billy.Filesystem // git repository root
	RFS  billy.Filesystem // root that contains user home
	MCFS billy.Filesystem // root that contains user home, but missing ~/.gitattributes
	MEFS billy.Filesystem // root that contains user home, but missing attributesfile entry
	MIFS billy.Filesystem // root that contains user home, but missing .gitattributes

	SFS billy.Filesystem // root that contains /etc/gitattributes
}

func TestMatcherSuite(t *testing.T) {
	suite.Run(t, new(MatcherSuite))
}

func (s *MatcherSuite) SetupTest() {
	home, err := os.UserHomeDir()
	s.NoError(err)

	gitAttributesGlobal := func(fs billy.Filesystem, filename string) {
		f, err := fs.Create(filename)
		s.NoError(err)
		_, err = f.Write([]byte("# IntelliJ\n"))
		s.NoError(err)
		_, err = f.Write([]byte(".idea/** text\n"))
		s.NoError(err)
		_, err = f.Write([]byte("*.iml -text\n"))
		s.NoError(err)
		err = f.Close()
		s.NoError(err)
	}

	// setup generic git repository root
	fs := memfs.New()
	f, err := fs.Create(".gitattributes")
	s.NoError(err)
	_, err = f.Write([]byte("vendor/g*/** foo=bar\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	err = fs.MkdirAll("vendor", os.ModePerm)
	s.NoError(err)
	f, err = fs.Create("vendor/.gitattributes")
	s.NoError(err)
	_, err = f.Write([]byte("github.com/** -foo\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	fs.MkdirAll("another", os.ModePerm)
	fs.MkdirAll("vendor/github.com", os.ModePerm)
	fs.MkdirAll("vendor/gopkg.in", os.ModePerm)

	gitAttributesGlobal(fs, fs.Join(home, ".gitattributes_global"))

	s.GFS = fs

	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, gitconfigFile))
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	_, err = f.Write([]byte("	attributesfile = " + strconv.Quote(fs.Join(home, ".gitattributes_global")) + "\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	gitAttributesGlobal(fs, fs.Join(home, ".gitattributes_global"))

	s.RFS = fs

	// root that contains user home, but missing ~/.gitconfig
	fs = memfs.New()
	gitAttributesGlobal(fs, fs.Join(home, ".gitattributes_global"))

	s.MCFS = fs

	// setup root that contains user home, but missing attributesfile entry
	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, gitconfigFile))
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	gitAttributesGlobal(fs, fs.Join(home, ".gitattributes_global"))

	s.MEFS = fs

	// setup root that contains user home, but missing .gitattributes
	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, gitconfigFile))
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	_, err = f.Write([]byte("	attributesfile = " + strconv.Quote(fs.Join(home, ".gitattributes_global")) + "\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	s.MIFS = fs

	// setup root that contains user home
	fs = memfs.New()
	err = fs.MkdirAll("etc", os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(systemFile)
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	_, err = f.Write([]byte("	attributesfile = /etc/gitattributes_global\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	gitAttributesGlobal(fs, "/etc/gitattributes_global")

	s.SFS = fs
}

func (s *MatcherSuite) TestDir_ReadPatterns() {
	ps, err := ReadPatterns(s.GFS, nil)
	s.NoError(err)
	s.Len(ps, 2)

	m := NewMatcher(ps)
	results, _ := m.Match([]string{"vendor", "gopkg.in", "file"}, nil)
	s.Equal("bar", results["foo"].Value())

	results, _ = m.Match([]string{"vendor", "github.com", "file"}, nil)
	s.False(results["foo"].IsUnset())
}

func (s *MatcherSuite) TestDir_LoadGlobalPatterns() {
	ps, err := LoadGlobalPatterns(s.RFS)
	s.NoError(err)
	s.Len(ps, 2)

	m := NewMatcher(ps)

	results, _ := m.Match([]string{"go-git.v4.iml"}, nil)
	s.True(results["text"].IsUnset())

	results, _ = m.Match([]string{".idea", "file"}, nil)
	s.True(results["text"].IsSet())
}

func (s *MatcherSuite) TestDir_LoadGlobalPatternsMissingGitconfig() {
	ps, err := LoadGlobalPatterns(s.MCFS)
	s.NoError(err)
	s.Len(ps, 0)
}

func (s *MatcherSuite) TestDir_LoadGlobalPatternsMissingAttributesfile() {
	ps, err := LoadGlobalPatterns(s.MEFS)
	s.NoError(err)
	s.Len(ps, 0)
}

func (s *MatcherSuite) TestDir_LoadGlobalPatternsMissingGitattributes() {
	ps, err := LoadGlobalPatterns(s.MIFS)
	s.NoError(err)
	s.Len(ps, 0)
}

func (s *MatcherSuite) TestDir_LoadSystemPatterns() {
	ps, err := LoadSystemPatterns(s.SFS)
	s.NoError(err)
	s.Len(ps, 2)

	m := NewMatcher(ps)
	results, _ := m.Match([]string{"go-git.v4.iml"}, nil)
	s.True(results["text"].IsUnset())

	results, _ = m.Match([]string{".idea", "file"}, nil)
	s.True(results["text"].IsSet())
}
