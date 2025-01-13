package gitignore

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

const (
	commentPrefix   = "#"
	coreSection     = "core"
	excludesfile    = "excludesfile"
	gitDir          = ".git"
	gitignoreFile   = ".gitignore"
	gitconfigFile   = ".gitconfig"
	systemFile      = "/etc/gitconfig"
	infoExcludeFile = gitDir + "/info/exclude"
)

// readIgnoreFile reads a specific git ignore file.
func readIgnoreFile(fs billy.Filesystem, path []string, ignoreFile string) (ps []Pattern, err error) {
	ignoreFile, _ = pathutil.ReplaceTildeWithHome(ignoreFile)

	f, err := fs.Open(fs.Join(append(path, ignoreFile)...))
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		s := scanner.Text()
		if !strings.HasPrefix(s, commentPrefix) && len(strings.TrimSpace(s)) > 0 {
			ps = append(ps, ParsePattern(s, path))
		}
	}
	return ps, scanner.Err()
}

// ReadPatterns reads the .git/info/exclude and then the gitignore patterns
// recursively traversing through the directory structure. The result is in
// the ascending order of priority (last higher).
func ReadPatterns(fs billy.Filesystem, path []string) (ps []Pattern, err error) {
	return extendPatterns(fs, nil, path)
}

func extendPatterns(fs billy.Filesystem, start []Pattern, path []string) (ps []Pattern, err error) {
	gitignore, err := readIgnoreFile(fs, path, gitignoreFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	infoExclude, err := readIgnoreFile(fs, path, infoExcludeFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	foundPatterns := append(gitignore, infoExclude...)

	ignorePatterns := append(start, foundPatterns...)
	ignoreMatcher := NewMatcher(ignorePatterns)

	fis, err := fs.ReadDir(fs.Join(path...))
	if err != nil {
		return nil, err
	}

	for _, fi := range fis {
		if fi.IsDir() && fi.Name() != gitDir {
			if ignoreMatcher.Match(append(path, fi.Name()), true) {
				continue
			}

			subPatterns, err := extendPatterns(fs, ignorePatterns, append(path, fi.Name()))
			if err != nil {
				return nil, err
			}

			if len(subPatterns) > 0 {
				foundPatterns = append(foundPatterns, subPatterns...)
			}
		}
	}

	return foundPatterns, err
}

func loadPatterns(fs billy.Filesystem, path string) (ps []Pattern, err error) {
	f, err := fs.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	defer ioutil.CheckClose(f, &err)

	b, err := io.ReadAll(f)
	if err != nil {
		return ps, err
	}

	d := config.NewDecoder(bytes.NewBuffer(b))

	raw := config.New()
	if err = d.Decode(raw); err != nil {
		return ps, err
	}

	s := raw.Section(coreSection)
	efo := s.Options.Get(excludesfile)
	if efo == "" {
		return nil, nil
	}

	ps, err = readIgnoreFile(fs, nil, efo)
	if os.IsNotExist(err) {
		return nil, nil
	}

	return ps, err
}

// LoadGlobalPatterns loads gitignore patterns from the gitignore file
// declared in a user's ~/.gitconfig file.  If the ~/.gitconfig file does not
// exist the function will return nil.  If the core.excludesfile property
// is not declared, the function will return nil.  If the file pointed to by
// the core.excludesfile property does not exist, the function will return nil.
//
// The function assumes fs is rooted at the root filesystem.
func LoadGlobalPatterns(fs billy.Filesystem) (ps []Pattern, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ps, err
	}

	return loadPatterns(fs, fs.Join(home, gitconfigFile))
}

// LoadSystemPatterns loads gitignore patterns from the gitignore file
// declared in a system's /etc/gitconfig file.  If the /etc/gitconfig file does
// not exist the function will return nil.  If the core.excludesfile property
// is not declared, the function will return nil.  If the file pointed to by
// the core.excludesfile property does not exist, the function will return nil.
//
// The function assumes fs is rooted at the root filesystem.
func LoadSystemPatterns(fs billy.Filesystem) (ps []Pattern, err error) {
	return loadPatterns(fs, systemFile)
}
