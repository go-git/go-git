package config

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAbs builds a platform-absolute path so the tests exercise the same
// filepath.IsAbs branches on POSIX and Windows.
func testAbs(parts ...string) string {
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

// slash renders a path the way it would be written inside a gitdir
// pattern, which always uses forward slashes.
func slash(p string) string {
	return filepath.ToSlash(p)
}

// openMap returns an Open function backed by an in-memory file set.
func openMap(files map[string]string) func(string) (io.ReadCloser, error) {
	return func(path string) (io.ReadCloser, error) {
		content, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("open %s: %w", path, fs.ErrNotExist)
		}
		return io.NopCloser(strings.NewReader(content)), nil
	}
}

func decode(t *testing.T, content string, opts *IncludeOptions) *Config {
	t.Helper()

	cfg := New()
	err := NewDecoderWithIncludes(bytes.NewBufferString(content), opts).Decode(cfg)
	require.NoError(t, err)

	return cfg
}

func TestIncludeNotFollowedByDefault(t *testing.T) {
	t.Parallel()

	root := testAbs("cfg")
	content := "[include]\n\tpath = " + slash(filepath.Join(root, "other")) + "\n"

	cfg := New()
	require.NoError(t, NewDecoder(bytes.NewBufferString(content)).Decode(cfg))

	// The directive itself is still visible as an ordinary option, the
	// way git reports it in `git config --list`.
	assert.Equal(t, slash(filepath.Join(root, "other")),
		cfg.Section("include").Option("path"))
	assert.False(t, cfg.HasSection("user"))
}

func TestIncludeAbsolutePath(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "included")
	opts := &IncludeOptions{
		Path: testAbs("cfg", "main"),
		Open: openMap(map[string]string{
			included: "[user]\n\tname = Included\n",
		}),
	}

	cfg := decode(t, "[include]\n\tpath = "+slash(included)+"\n", opts)
	assert.Equal(t, "Included", cfg.Section("user").Option("name"))
}

func TestIncludeRelativePathResolvesAgainstIncludingFile(t *testing.T) {
	t.Parallel()

	opts := &IncludeOptions{
		Path: testAbs("cfg", "main"),
		Open: openMap(map[string]string{
			testAbs("cfg", "sub", "extra"): "[user]\n\temail = rel@example.com\n",
		}),
	}

	cfg := decode(t, "[include]\n\tpath = sub/extra\n", opts)
	assert.Equal(t, "rel@example.com", cfg.Section("user").Option("email"))
}

func TestIncludeRelativePathWithoutFileIsAnError(t *testing.T) {
	t.Parallel()

	opts := &IncludeOptions{Open: openMap(nil)}

	cfg := New()
	err := NewDecoderWithIncludes(
		bytes.NewBufferString("[include]\n\tpath = sub/extra\n"), opts,
	).Decode(cfg)

	assert.ErrorIs(t, err, ErrRelativeIncludeWithoutFile)
}

func TestIncludeTildeExpansion(t *testing.T) {
	t.Parallel()

	home := testAbs("home", "u")
	opts := &IncludeOptions{
		Path: testAbs("cfg", "main"),
		Home: home,
		Open: openMap(map[string]string{
			filepath.Join(home, "work.inc"): "[user]\n\tname = Tilde\n",
		}),
	}

	cfg := decode(t, "[include]\n\tpath = ~/work.inc\n", opts)
	assert.Equal(t, "Tilde", cfg.Section("user").Option("name"))
}

// git-config(1): an included file's values behave as if inlined at the
// point of the include directive.
func TestIncludePrecedenceIsPositional(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "inc")
	files := map[string]string{included: "[user]\n\tname = FromInclude\n"}

	t.Run("include overrides earlier value", func(t *testing.T) {
		t.Parallel()
		opts := &IncludeOptions{Path: testAbs("cfg", "main"), Open: openMap(files)}
		cfg := decode(t,
			"[user]\n\tname = Before\n[include]\n\tpath = "+slash(included)+"\n", opts)
		assert.Equal(t, "FromInclude", cfg.Section("user").Option("name"))
	})

	t.Run("later value overrides include", func(t *testing.T) {
		t.Parallel()
		opts := &IncludeOptions{Path: testAbs("cfg", "main"), Open: openMap(files)}
		cfg := decode(t,
			"[include]\n\tpath = "+slash(included)+"\n[user]\n\tname = After\n", opts)
		assert.Equal(t, "After", cfg.Section("user").Option("name"))
	})
}

func TestIncludeNested(t *testing.T) {
	t.Parallel()

	first := testAbs("cfg", "first")
	second := testAbs("cfg", "second")

	opts := &IncludeOptions{
		Path: testAbs("cfg", "main"),
		Open: openMap(map[string]string{
			// The nested include uses a relative path, which must
			// resolve against the file that contains it.
			first:  "[include]\n\tpath = second\n",
			second: "[user]\n\tname = Deep\n",
		}),
	}

	cfg := decode(t, "[include]\n\tpath = "+slash(first)+"\n", opts)
	assert.Equal(t, "Deep", cfg.Section("user").Option("name"))
}

// A section or subsection an included file creates is the one the including
// file adds to when it names it again, not a second one beside it.
func TestIncludeSectionsAreSharedWithIncludingFile(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "inc")
	opts := &IncludeOptions{
		Path: testAbs("cfg", "main"),
		Open: openMap(map[string]string{
			included: "[user]\n\tname = FromInclude\n[remote \"origin\"]\n\turl = a\n",
		}),
	}

	cfg := decode(t, "[include]\n\tpath = "+slash(included)+"\n"+
		"[user]\n\temail = after@example.com\n[remote \"origin\"]\n\tfetch = b\n", opts)

	var users, remotes int
	for _, s := range cfg.Sections {
		switch s.Name {
		case "user":
			users++
		case "remote":
			remotes++
		}
	}
	assert.Equal(t, 1, users)
	require.Equal(t, 1, remotes)
	assert.Equal(t, "FromInclude", cfg.Section("user").Option("name"))
	assert.Equal(t, "after@example.com", cfg.Section("user").Option("email"))
	assert.Len(t, cfg.Section("remote").Subsections, 1)
	origin := cfg.Section("remote").Subsection("origin")
	assert.Equal(t, "a", origin.Option("url"))
	assert.Equal(t, "b", origin.Option("fetch"))
}

func TestIncludeMissingFileIsSkipped(t *testing.T) {
	t.Parallel()

	opts := &IncludeOptions{
		Path: testAbs("cfg", "main"),
		Open: openMap(map[string]string{}),
	}

	cfg := decode(t,
		"[include]\n\tpath = "+slash(testAbs("cfg", "absent"))+"\n[user]\n\tname = Still\n", opts)
	assert.Equal(t, "Still", cfg.Section("user").Option("name"))
}

func TestIncludeCycleIsBounded(t *testing.T) {
	t.Parallel()

	a := testAbs("cfg", "a")
	opts := &IncludeOptions{
		Path: a,
		Open: openMap(map[string]string{
			a: "[include]\n\tpath = " + slash(a) + "\n",
		}),
	}

	cfg := New()
	err := NewDecoderWithIncludes(
		bytes.NewBufferString("[include]\n\tpath = "+slash(a)+"\n"), opts,
	).Decode(cfg)

	assert.ErrorIs(t, err, ErrIncludeDepthExceeded)
}

func TestIncludeIfGitDir(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "work")
	files := map[string]string{included: "[user]\n\temail = work@example.com\n"}

	tests := []struct {
		name      string
		condition string
		gitDir    string
		want      bool
	}{
		{
			name:      "trailing slash matches everything below",
			condition: "gitdir:" + slash(testAbs("src", "work")) + "/",
			gitDir:    testAbs("src", "work", "repo", ".git"),
			want:      true,
		},
		{
			name:      "trailing slash does not match a sibling",
			condition: "gitdir:" + slash(testAbs("src", "work")) + "/",
			gitDir:    testAbs("src", "personal", "repo", ".git"),
			want:      false,
		},
		{
			name:      "bare name is prefixed with **/",
			condition: "gitdir:work/",
			gitDir:    testAbs("src", "work", "repo", ".git"),
			want:      true,
		},
		{
			name:      "case sensitive by default",
			condition: "gitdir:" + slash(testAbs("src", "WORK")) + "/",
			gitDir:    testAbs("src", "work", "repo", ".git"),
			want:      false,
		},
		{
			name:      "exact path without trailing slash",
			condition: "gitdir:" + slash(testAbs("src", "work", "repo", ".git")),
			gitDir:    testAbs("src", "work", "repo", ".git"),
			want:      true,
		},
		{
			name:      "single star does not cross a separator",
			condition: "gitdir:" + slash(testAbs("src")) + "/*/.git",
			gitDir:    testAbs("src", "a", "b", ".git"),
			want:      false,
		},
		{
			name:      "double star crosses separators",
			condition: "gitdir:" + slash(testAbs("src")) + "/**/.git",
			gitDir:    testAbs("src", "a", "b", ".git"),
			want:      true,
		},
		{
			name:      "empty pattern matches everything",
			condition: "gitdir:",
			gitDir:    testAbs("src", "work", "repo", ".git"),
			want:      true,
		},
		{
			name:      "no gitdir means false",
			condition: "gitdir:" + slash(testAbs("src")) + "/",
			gitDir:    "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &IncludeOptions{
				Path:   testAbs("cfg", "main"),
				GitDir: tt.gitDir,
				Open:   openMap(files),
			}

			cfg := decode(t,
				"[includeIf \""+tt.condition+"\"]\n\tpath = "+slash(included)+"\n", opts)

			if tt.want {
				assert.Equal(t, "work@example.com", cfg.Section("user").Option("email"))
			} else {
				assert.Empty(t, cfg.Section("user").Option("email"))
			}
		})
	}
}

func TestIncludeIfGitDirCaseInsensitive(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "work")
	opts := &IncludeOptions{
		Path:   testAbs("cfg", "main"),
		GitDir: testAbs("src", "work", "repo", ".git"),
		Open:   openMap(map[string]string{included: "[user]\n\temail = i@example.com\n"}),
	}

	condition := "gitdir/i:" + slash(testAbs("src", "WORK")) + "/"
	cfg := decode(t, "[includeIf \""+condition+"\"]\n\tpath = "+slash(included)+"\n", opts)

	assert.Equal(t, "i@example.com", cfg.Section("user").Option("email"))
}

func TestIncludeIfGitDirRelativeToConfigFile(t *testing.T) {
	t.Parallel()

	included := testAbs("src", "work", "extra")
	opts := &IncludeOptions{
		Path:   testAbs("src", "work", "gitconfig"),
		GitDir: testAbs("src", "work", "repo", ".git"),
		Open:   openMap(map[string]string{included: "[user]\n\temail = rel@example.com\n"}),
	}

	cfg := decode(t, "[includeIf \"gitdir:./\"]\n\tpath = extra\n", opts)
	assert.Equal(t, "rel@example.com", cfg.Section("user").Option("email"))
}

func TestIncludeIfWorkTree(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "work")
	files := map[string]string{included: "[user]\n\temail = work@example.com\n"}

	tests := []struct {
		name      string
		condition string
		workTree  string
		want      bool
	}{
		{
			name:      "exact path",
			condition: "worktree:" + slash(testAbs("src", "work", "repo")),
			workTree:  testAbs("src", "work", "repo"),
			want:      true,
		},
		{
			name:      "trailing slash matches only below the path",
			condition: "worktree:" + slash(testAbs("src", "work", "repo")) + "/",
			workTree:  testAbs("src", "work", "repo"),
			want:      false,
		},
		{
			name:      "trailing slash matches everything below",
			condition: "worktree:" + slash(testAbs("src", "work")) + "/",
			workTree:  testAbs("src", "work", "repo"),
			want:      true,
		},
		{
			name:      "bare name is prefixed with **/",
			condition: "worktree:repo",
			workTree:  testAbs("src", "work", "repo"),
			want:      true,
		},
		{
			name:      "the git directory is not the working tree",
			condition: "worktree:" + slash(testAbs("src", "work", "repo", ".git")),
			workTree:  testAbs("src", "work", "repo"),
			want:      false,
		},
		{
			name:      "case sensitive by default",
			condition: "worktree:" + slash(testAbs("src", "WORK")) + "/",
			workTree:  testAbs("src", "work", "repo"),
			want:      false,
		},
		{
			name:      "case insensitive variant",
			condition: "worktree/i:" + slash(testAbs("src", "WORK")) + "/",
			workTree:  testAbs("src", "work", "repo"),
			want:      true,
		},
		{
			name:      "bare repository means false",
			condition: "worktree:",
			workTree:  "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &IncludeOptions{
				Path:     testAbs("cfg", "main"),
				GitDir:   testAbs("src", "work", "repo", ".git"),
				WorkTree: tt.workTree,
				Open:     openMap(files),
			}

			cfg := decode(t,
				"[includeIf \""+tt.condition+"\"]\n\tpath = "+slash(included)+"\n", opts)

			if tt.want {
				assert.Equal(t, "work@example.com", cfg.Section("user").Option("email"))
			} else {
				assert.Empty(t, cfg.Section("user").Option("email"))
			}
		})
	}
}

func TestIncludeIfOnBranch(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "branch")
	files := map[string]string{included: "[user]\n\temail = branch@example.com\n"}

	tests := []struct {
		name      string
		condition string
		branch    string
		want      bool
	}{
		{"exact match", "onbranch:main", "main", true},
		{"no match", "onbranch:main", "topic", false},
		{"trailing slash matches below", "onbranch:feature/", "feature/x", true},
		{"trailing slash matches nested", "onbranch:feature/", "feature/a/b", true},
		{"star does not cross slash", "onbranch:feature/*", "feature/a/b", false},
		{"detached head never matches", "onbranch:main", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &IncludeOptions{
				Path:   testAbs("cfg", "main"),
				Branch: tt.branch,
				Open:   openMap(files),
			}

			cfg := decode(t,
				"[includeIf \""+tt.condition+"\"]\n\tpath = "+slash(included)+"\n", opts)

			if tt.want {
				assert.Equal(t, "branch@example.com", cfg.Section("user").Option("email"))
			} else {
				assert.Empty(t, cfg.Section("user").Option("email"))
			}
		})
	}
}

func TestIncludeIfHasConfigRemoteURL(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "remote")
	files := map[string]string{included: "[user]\n\temail = remote@example.com\n"}

	tests := []struct {
		name      string
		condition string
		urls      []string
		want      bool
	}{
		{
			name:      "glob matches one of several remotes",
			condition: "hasconfig:remote.*.url:git@github.com:work/**",
			urls:      []string{"git@github.com:personal/x.git", "git@github.com:work/y.git"},
			want:      true,
		},
		{
			name:      "no remote matches",
			condition: "hasconfig:remote.*.url:git@github.com:work/**",
			urls:      []string{"git@github.com:personal/x.git"},
			want:      false,
		},
		{
			name:      "no remotes at all",
			condition: "hasconfig:remote.*.url:**",
			urls:      nil,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &IncludeOptions{
				Path:       testAbs("cfg", "main"),
				RemoteURLs: tt.urls,
				Open:       openMap(files),
			}

			cfg := decode(t,
				"[includeIf \""+tt.condition+"\"]\n\tpath = "+slash(included)+"\n", opts)

			if tt.want {
				assert.Equal(t, "remote@example.com", cfg.Section("user").Option("email"))
			} else {
				assert.Empty(t, cfg.Section("user").Option("email"))
			}
		})
	}
}

// git treats conditions it does not recognise as false rather than
// failing, so that configs written for newer versions still load.
func TestIncludeIfUnknownConditionIsFalse(t *testing.T) {
	t.Parallel()

	included := testAbs("cfg", "x")
	opts := &IncludeOptions{
		Path: testAbs("cfg", "main"),
		Open: openMap(map[string]string{included: "[user]\n\tname = Nope\n"}),
	}

	cfg := decode(t,
		"[includeIf \"onsolarflare:high\"]\n\tpath = "+slash(included)+"\n", opts)

	assert.Empty(t, cfg.Section("user").Option("name"))
}

// openOS opens included files on the host filesystem, as git does.
func openOS(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

// symlinkedRoot returns a temporary directory, with symlinks resolved, in
// which link points at target.
func symlinkedRoot(t *testing.T) (root, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs privileges on Windows")
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	target = filepath.Join(root, "real")
	link = filepath.Join(root, "link")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.Symlink(target, link))
	return root, target, link
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestIncludeTildeThatCannotBeExpandedIsAnError(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"~/inc", "~no-such-user-go-git/inc"} {
		opts := &IncludeOptions{Path: testAbs("cfg", "main"), Open: openMap(nil)}
		err := NewDecoderWithIncludes(
			bytes.NewBufferString("[include]\n\tpath = "+path+"\n"), opts,
		).Decode(New())
		assert.ErrorIs(t, err, ErrIncludePathNotExpanded, path)
	}
}

// Git skips an include that does not exist, but not one it cannot read.
func TestIncludeUnreadableFileIsAnError(t *testing.T) {
	t.Parallel()

	opts := &IncludeOptions{
		Path: testAbs("cfg", "main"),
		Open: func(path string) (io.ReadCloser, error) {
			return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission}
		},
	}
	err := NewDecoderWithIncludes(
		bytes.NewBufferString("[include]\n\tpath = "+slash(testAbs("cfg", "inc"))+"\n"), opts,
	).Decode(New())
	assert.ErrorIs(t, err, fs.ErrPermission)
}

// A path through a file, as if it were a directory, is skipped like a
// missing one.
func TestIncludeThroughNonDirectoryIsSkipped(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "plan9" {
		t.Skip("Plan 9 has no ENOTDIR")
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "file"), "")
	opts := &IncludeOptions{Path: filepath.Join(dir, "main"), Open: openOS}

	cfg := decode(t, "[include]\n\tpath = file/inc\n[user]\n\tname = Main\n", opts)
	assert.Equal(t, "Main", cfg.Section("user").Option("name"))
}

// The depth limit counts the files that are read, so a missing include
// past it is skipped like any other.
func TestIncludeDepthCountsOnlyFilesRead(t *testing.T) {
	t.Parallel()

	files := map[string]string{}
	for i := 1; i <= DefaultMaxIncludeDepth; i++ {
		files[testAbs("cfg", fmt.Sprint(i))] = fmt.Sprintf("[include]\n\tpath = %d\n", i+1)
	}
	opts := &IncludeOptions{Path: testAbs("cfg", "main"), Open: openMap(files)}

	cfg := New()
	err := NewDecoderWithIncludes(bytes.NewBufferString("[include]\n\tpath = 1\n"), opts).Decode(cfg)
	assert.NoError(t, err)
}

// Git leaves ".." in a relative include path to the filesystem, which
// resolves it after the symlink before it rather than by dropping the
// symlink's name.
func TestIncludeRelativePathResolvesDotDotAfterSymlinks(t *testing.T) {
	t.Parallel()

	root, target, _ := symlinkedRoot(t)
	// home/work is a symlink to real/work.
	require.NoError(t, os.MkdirAll(filepath.Join(target, "work"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "home"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(target, "work"), filepath.Join(root, "home", "work")))
	writeFile(t, filepath.Join(target, "x"), "[w]\n\twhere = real\n")
	writeFile(t, filepath.Join(root, "home", "x"), "[w]\n\twhere = home\n")

	opts := &IncludeOptions{Path: filepath.Join(root, "home", "work", "cfg"), Open: openOS}
	cfg := decode(t, "[include]\n\tpath = ../x\n", opts)
	assert.Equal(t, "real", cfg.Section("w").Option("where"))
}

// Git matches a gitdir: pattern against the target path of the git
// directory and then against the path as given, so that a pattern naming
// either side of a symlink matches.
func TestIncludeIfGitDirThroughSymlink(t *testing.T) {
	t.Parallel()

	root, target, link := symlinkedRoot(t)
	included := filepath.Join(root, "inc")
	writeFile(t, included, "[user]\n\temail = work@example.com\n")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "repo", ".git"), 0o755))

	for _, tt := range []struct{ name, pattern, gitDir string }{
		{"pattern names the symlink", link + "/", filepath.Join(link, "repo", ".git")},
		{"pattern names the target path", target + "/", filepath.Join(link, "repo", ".git")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &IncludeOptions{Path: filepath.Join(root, "main"), GitDir: tt.gitDir, Open: openOS}
			cfg := decode(t, "[includeIf \"gitdir:"+slash(tt.pattern)+"\"]\n\tpath = "+slash(included)+"\n", opts)
			assert.Equal(t, "work@example.com", cfg.Section("user").Option("email"))
		})
	}
}

// Unlike gitdir:, a worktree: pattern is matched only against the target
// path, because git resolves the working tree when it sets up the
// repository.
func TestIncludeIfWorkTreeThroughSymlink(t *testing.T) {
	t.Parallel()

	root, target, link := symlinkedRoot(t)
	included := filepath.Join(root, "inc")
	writeFile(t, included, "[user]\n\temail = work@example.com\n")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "repo"), 0o755))

	for _, tt := range []struct {
		name, pattern string
		want          bool
	}{
		{"pattern names the symlink", link + "/", false},
		{"pattern names the target path", target + "/", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &IncludeOptions{Path: filepath.Join(root, "main"), WorkTree: filepath.Join(link, "repo"), Open: openOS}
			cfg := decode(t, "[includeIf \"worktree:"+slash(tt.pattern)+"\"]\n\tpath = "+slash(included)+"\n", opts)
			if tt.want {
				assert.Equal(t, "work@example.com", cfg.Section("user").Option("email"))
			} else {
				assert.Empty(t, cfg.Section("user").Option("email"))
			}
		})
	}
}

// "~/" in a gitdir: pattern stands for the target path of the home
// directory, and "./" for the target directory of the including file.
func TestIncludeIfGitDirUsesRealPaths(t *testing.T) {
	t.Parallel()

	root, target, link := symlinkedRoot(t)
	included := filepath.Join(root, "inc")
	writeFile(t, included, "[user]\n\temail = work@example.com\n")
	gitDir := filepath.Join(target, "work", "repo", ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))

	t.Run("home", func(t *testing.T) {
		t.Parallel()
		opts := &IncludeOptions{Path: filepath.Join(root, "main"), Home: link, GitDir: gitDir, Open: openOS}
		cfg := decode(t, "[includeIf \"gitdir:~/work/\"]\n\tpath = "+slash(included)+"\n", opts)
		assert.Equal(t, "work@example.com", cfg.Section("user").Option("email"))
	})
	t.Run("including file", func(t *testing.T) {
		t.Parallel()
		writeFile(t, filepath.Join(target, "cfg"), "")
		opts := &IncludeOptions{Path: filepath.Join(link, "cfg"), GitDir: gitDir, Open: openOS}
		cfg := decode(t, "[includeIf \"gitdir:./work/\"]\n\tpath = "+slash(included)+"\n", opts)
		assert.Equal(t, "work@example.com", cfg.Section("user").Option("email"))
	})
}

// While remote URLs are collected for hasconfig:remote.*.url: conditions,
// a file a true includeIf reaches, directly or through a plain include,
// may not set one, as git refuses those.
func TestIncludeIfForbidsRemoteURLWhileCollecting(t *testing.T) {
	t.Parallel()

	remote := "[remote \"origin\"]\n\turl = https://example.com/r.git\n"
	files := map[string]string{
		testAbs("cfg", "remote"):   remote,
		testAbs("cfg", "indirect"): "[include]\n\tpath = remote\n",
	}
	conditional := func(path string) string {
		return "[includeIf \"hasconfig:remote.*.url:https://example.com/**\"]\n\tpath = " + path + "\n"
	}

	for _, tt := range []struct {
		name, content string
		collecting    bool
		wantErr       bool
	}{
		{"directly", conditional("remote"), true, true},
		{"indirectly", conditional("indirect"), true, true},
		{"through a plain include", "[include]\n\tpath = remote\n", true, false},
		{"when not collecting", conditional("remote"), false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &IncludeOptions{
				Path:                   testAbs("cfg", "main"),
				Open:                   openMap(files),
				RemoteURLs:             []string{"https://example.com/r.git"},
				UnconditionalRemoteURL: tt.collecting,
			}
			err := NewDecoderWithIncludes(bytes.NewBufferString(tt.content), opts).Decode(New())
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrRemoteURLInConditionalInclude)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func FuzzDecoderWithIncludes(f *testing.F) {
	f.Add([]byte("[include]\n\tpath = inc\n"), []byte("[user]\n\tname = a\n"), false)
	f.Add([]byte("[includeIf \"gitdir:**/r/\"]\n\tpath = ~/inc\n"), []byte("[include]\n\tpath = ../inc\n"), false)
	f.Add([]byte("[includeIf \"hasconfig:remote.*.url:https://**\"]\n\tpath = inc\n"),
		[]byte("[remote \"o\"]\n\turl = https://example.com\n"), true)
	f.Add([]byte("[includeIf \"worktree/i:./[a-z]*/\"]\n\tpath = inc\n[includeIf \"onbranch:m*\"]\n\tpath = inc\n"),
		[]byte("[include]\n\tpath = inc\n\tpath = inc\n"), false)

	f.Fuzz(func(_ *testing.T, root, included []byte, unconditionalRemoteURL bool) {
		// Every path opens the same file, so an include that names itself
		// more than once fans out at every level; the budget keeps each
		// run bounded.
		opens := 0
		opts := &IncludeOptions{
			Open: func(string) (io.ReadCloser, error) {
				if opens++; opens > 100 {
					return nil, fs.ErrNotExist
				}
				return io.NopCloser(bytes.NewReader(included)), nil
			},
			Path:                   testAbs("home", "u", "r", ".git", "config"),
			Home:                   testAbs("home", "u"),
			GitDir:                 testAbs("home", "u", "r", ".git"),
			WorkTree:               testAbs("home", "u", "r"),
			Branch:                 "main",
			RemoteURLs:             []string{"https://example.com/r.git"},
			UnconditionalRemoteURL: unconditionalRemoteURL,
		}
		_ = NewDecoderWithIncludes(bytes.NewReader(root), opts).Decode(New())
	})
}
