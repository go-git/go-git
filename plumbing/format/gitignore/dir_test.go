package gitignore

import (
	"os"
	"os/user"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/suite"
)

type MatcherSuite struct {
	suite.Suite
	GFS  billy.Filesystem // git repository root
	RFS  billy.Filesystem // root that contains user home
	RFSR billy.Filesystem // root that contains user home, but with relative ~/.gitignore_global
	RFSU billy.Filesystem // root that contains user home, but with relative ~user/.gitignore_global
	MCFS billy.Filesystem // root that contains user home, but missing ~/.gitconfig
	MEFS billy.Filesystem // root that contains user home, but missing excludesfile entry
	MIFS billy.Filesystem // root that contains user home, but missing .gitignore

	SFS billy.Filesystem // root that contains /etc/gitconfig
}

func TestMatcherSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(MatcherSuite))
}

func (s *MatcherSuite) SetupTest() {
	// setup generic git repository root
	fs := memfs.New()

	err := fs.MkdirAll(".git/info", os.ModePerm)
	s.NoError(err)
	f, err := fs.Create(".git/info/exclude")
	s.NoError(err)
	_, err = f.Write([]byte("exclude.crlf\r\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	f, err = fs.Create(".gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("vendor/g*/\n"))
	s.NoError(err)
	_, err = f.Write([]byte("ignore.crlf\r\n"))
	s.NoError(err)
	_, err = f.Write([]byte("/ignore_dir\n"))
	s.NoError(err)
	_, err = f.Write([]byte("nested/ignore_dir\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	err = fs.MkdirAll("vendor", os.ModePerm)
	s.NoError(err)
	f, err = fs.Create("vendor/.gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("!github.com/\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	err = fs.MkdirAll("ignore_dir", os.ModePerm)
	s.NoError(err)
	f, err = fs.Create("ignore_dir/.gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("!file\n"))
	s.NoError(err)
	_, err = fs.Create("ignore_dir/file")
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	err = fs.MkdirAll("nested/ignore_dir", os.ModePerm)
	s.NoError(err)
	f, err = fs.Create("nested/ignore_dir/.gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("!file\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)
	_, err = fs.Create("nested/ignore_dir/file")
	s.NoError(err)

	err = fs.MkdirAll("another", os.ModePerm)
	s.NoError(err)
	err = fs.MkdirAll("exclude.crlf", os.ModePerm)
	s.NoError(err)
	err = fs.MkdirAll("ignore.crlf", os.ModePerm)
	s.NoError(err)
	err = fs.MkdirAll("vendor/github.com", os.ModePerm)
	s.NoError(err)
	err = fs.MkdirAll("vendor/gopkg.in", os.ModePerm)
	s.NoError(err)

	err = fs.MkdirAll("multiple/sub/ignores/first", os.ModePerm)
	s.NoError(err)
	err = fs.MkdirAll("multiple/sub/ignores/second", os.ModePerm)
	s.NoError(err)
	f, err = fs.Create("multiple/sub/ignores/first/.gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("ignore_dir\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)
	f, err = fs.Create("multiple/sub/ignores/second/.gitignore")
	s.NoError(err)
	_, err = f.Write([]byte("ignore_dir\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)
	err = fs.MkdirAll("multiple/sub/ignores/first/ignore_dir", os.ModePerm)
	s.NoError(err)
	err = fs.MkdirAll("multiple/sub/ignores/second/ignore_dir", os.ModePerm)
	s.NoError(err)

	s.GFS = fs

	// setup root that contains user home
	home, err := os.UserHomeDir()
	s.NoError(err)

	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, gitconfigFile))
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	_, err = f.Write([]byte("	excludesfile = " + strconv.Quote(fs.Join(home, ".gitignore_global")) + "\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, ".gitignore_global"))
	s.NoError(err)
	_, err = f.Write([]byte("# IntelliJ\n"))
	s.NoError(err)
	_, err = f.Write([]byte(".idea/\n"))
	s.NoError(err)
	_, err = f.Write([]byte("*.iml\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	s.RFS = fs

	// root that contains user home, but with relative ~/.gitignore_global
	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, gitconfigFile))
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	_, err = f.Write([]byte("	excludesfile = ~/.gitignore_global" + "\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, ".gitignore_global"))
	s.NoError(err)
	_, err = f.Write([]byte("# IntelliJ\n"))
	s.NoError(err)
	_, err = f.Write([]byte(".idea/\n"))
	s.NoError(err)
	_, err = f.Write([]byte("*.iml\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	s.RFSR = fs

	// root that contains user home, but with relative ~user/.gitignore_global
	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, gitconfigFile))
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	currentUser, err := user.Current()
	s.NoError(err)
	// remove domain for windows
	username := currentUser.Username[strings.Index(currentUser.Username, "\\")+1:]
	_, err = f.Write([]byte("	excludesfile = ~" + username + "/.gitignore_global" + "\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, ".gitignore_global"))
	s.NoError(err)
	_, err = f.Write([]byte("# IntelliJ\n"))
	s.NoError(err)
	_, err = f.Write([]byte(".idea/\n"))
	s.NoError(err)
	_, err = f.Write([]byte("*.iml\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	s.RFSU = fs

	// root that contains user home, but missing ~/.gitconfig
	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, ".gitignore_global"))
	s.NoError(err)
	_, err = f.Write([]byte("# IntelliJ\n"))
	s.NoError(err)
	_, err = f.Write([]byte(".idea/\n"))
	s.NoError(err)
	_, err = f.Write([]byte("*.iml\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	s.MCFS = fs

	// setup root that contains user home, but missing excludesfile entry
	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, gitconfigFile))
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, ".gitignore_global"))
	s.NoError(err)
	_, err = f.Write([]byte("# IntelliJ\n"))
	s.NoError(err)
	_, err = f.Write([]byte(".idea/\n"))
	s.NoError(err)
	_, err = f.Write([]byte("*.iml\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	s.MEFS = fs

	// setup root that contains user home, but missing .gitnignore
	fs = memfs.New()
	err = fs.MkdirAll(home, os.ModePerm)
	s.NoError(err)

	f, err = fs.Create(fs.Join(home, gitconfigFile))
	s.NoError(err)
	_, err = f.Write([]byte("[core]\n"))
	s.NoError(err)
	_, err = f.Write([]byte("	excludesfile = " + strconv.Quote(fs.Join(home, ".gitignore_global")) + "\n"))
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
	_, err = f.Write([]byte("	excludesfile = /etc/gitignore_global\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	f, err = fs.Create("/etc/gitignore_global")
	s.NoError(err)
	_, err = f.Write([]byte("# IntelliJ\n"))
	s.NoError(err)
	_, err = f.Write([]byte(".idea/\n"))
	s.NoError(err)
	_, err = f.Write([]byte("*.iml\n"))
	s.NoError(err)
	err = f.Close()
	s.NoError(err)

	s.SFS = fs
}

func (s *MatcherSuite) TestDir_ReadPatterns() {
	checkPatterns := func(ps []Pattern) {
		s.Len(ps, 8)
		m := NewMatcher(ps)

		s.True(m.Match([]string{"exclude.crlf"}, true))
		s.True(m.Match([]string{"ignore.crlf"}, true))
		s.True(m.Match([]string{"vendor", "gopkg.in"}, true))
		s.True(m.Match([]string{"ignore_dir", "file"}, false))
		s.True(m.Match([]string{"nested", "ignore_dir", "file"}, false))
		s.False(m.Match([]string{"vendor", "github.com"}, true))
		s.True(m.Match([]string{"multiple", "sub", "ignores", "first", "ignore_dir"}, true))
		s.True(m.Match([]string{"multiple", "sub", "ignores", "second", "ignore_dir"}, true))
	}

	ps, err := ReadPatterns(s.GFS, nil)
	s.NoError(err)
	checkPatterns(ps)

	// passing an empty slice with capacity to check we don't hit a bug where the extra capacity is reused incorrectly
	ps, err = ReadPatterns(s.GFS, make([]string, 0, 6))
	s.NoError(err)
	checkPatterns(ps)
}

func (s *MatcherSuite) TestDir_ReadRelativeGlobalGitIgnore() {
	for _, fs := range []billy.Filesystem{s.RFSR, s.RFSU} {
		ps, err := LoadGlobalPatterns(fs)
		s.NoError(err)
		s.Len(ps, 2)

		m := NewMatcher(ps)
		s.False(m.Match([]string{".idea/"}, true))
		s.True(m.Match([]string{"*.iml"}, true))
		s.False(m.Match([]string{"IntelliJ"}, true))
	}
}

func (s *MatcherSuite) TestDir_LoadGlobalPatterns() {
	ps, err := LoadGlobalPatterns(s.RFS)
	s.NoError(err)
	s.Len(ps, 2)

	m := NewMatcher(ps)
	s.True(m.Match([]string{"go-git.v4.iml"}, true))
	s.True(m.Match([]string{".idea"}, true))
}

func (s *MatcherSuite) TestDir_LoadGlobalPatternsMissingGitconfig() {
	ps, err := LoadGlobalPatterns(s.MCFS)
	s.NoError(err)
	s.Len(ps, 0)
}

func (s *MatcherSuite) TestDir_LoadGlobalPatternsMissingExcludesfile() {
	ps, err := LoadGlobalPatterns(s.MEFS)
	s.NoError(err)
	s.Len(ps, 0)
}

func (s *MatcherSuite) TestDir_LoadGlobalPatternsMissingGitignore() {
	ps, err := LoadGlobalPatterns(s.MIFS)
	s.NoError(err)
	s.Len(ps, 0)
}

func (s *MatcherSuite) TestDir_LoadSystemPatterns() {
	ps, err := LoadSystemPatterns(s.SFS)
	s.NoError(err)
	s.Len(ps, 2)

	m := NewMatcher(ps)
	s.True(m.Match([]string{"go-git.v4.iml"}, true))
	s.True(m.Match([]string{".idea"}, true))
}
