package gitignore

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/internal/pathutil"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	gioutil "github.com/go-git/go-git/v6/utils/ioutil"
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
	if err == nil {
		defer func() { _ = f.Close() }()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			s := scanner.Text()
			if !strings.HasPrefix(s, commentPrefix) && len(strings.TrimSpace(s)) > 0 {
				ps = append(ps, ParsePattern(s, path))
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	return ps, err
}

// ReadPatterns reads the .git/info/exclude and then the gitignore patterns
// recursively traversing through the directory structure. The result is in
// the ascending order of priority (last higher).
//
// .git/info/exclude is only consulted at the root of the given filesystem,
// matching reference git which reads $GIT_DIR/info/exclude of the
// repository being walked. Ignore files are opened only when present in
// the directory listing, so directories without them cost a single ReadDir.
//
// Excluded directories are not descended into, at any depth below the ignore
// file declaring the rule, so patterns declared inside an excluded directory
// are not read and do not appear in the result. See the package documentation
// for why gitignore(5) makes that equivalent to reading them.
func ReadPatterns(fs billy.Filesystem, path []string) (ps []Pattern, err error) {
	return readPatterns(fs, path, nil)
}

// readPatterns collects the ignore patterns declared at path and below.
//
// inherited holds the patterns already in effect from ancestor directories in
// ascending order of priority. They take part in the decision to descend into
// a subdirectory, but are not returned, so each pattern reaches the caller
// exactly once.
func readPatterns(fs billy.Filesystem, path []string, inherited []Pattern) (ps []Pattern, err error) {
	fis, err := fs.ReadDir(fs.Join(path...))
	if err != nil {
		return nil, err
	}

	var hasGitDir, hasGitignore bool
	for _, fi := range fis {
		switch fi.Name() {
		case gitDir:
			hasGitDir = true
		case gitignoreFile:
			hasGitignore = true
		}
	}

	if len(path) == 0 && hasGitDir {
		ps, _ = readIgnoreFile(fs, path, infoExcludeFile)
	}

	if hasGitignore {
		subps, _ := readIgnoreFile(fs, path, gitignoreFile)
		ps = append(ps, subps...)
	}

	// Ancestors first, so the deeper file wins. This deliberately excludes the
	// patterns that the loop below collects from sibling subtrees: they cannot
	// apply here, and including them would make the walk order-dependent.
	inScope := inherited
	if len(ps) > 0 {
		inScope = slices.Concat(inherited, ps)
	}
	m := NewMatcher(inScope)

	for _, fi := range fis {
		if !fi.IsDir() || fi.Name() == gitDir {
			continue
		}

		sub := slices.Concat(path, []string{fi.Name()})
		if m.Match(sub, true) {
			continue
		}

		subps, err := readPatterns(fs, sub, inScope)
		if err != nil {
			return ps, err
		}

		ps = append(ps, subps...)
	}

	return ps, nil
}

func loadPatterns(fs billy.Filesystem, path string) (ps []Pattern, err error) {
	f, err := fs.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	defer gioutil.CheckClose(f, &err)

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
