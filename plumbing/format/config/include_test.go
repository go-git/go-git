package config

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
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
		bytes.NewBufferString("[include]\n\tpath = sub/extra\n"), opts).Decode(cfg)

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
		bytes.NewBufferString("[include]\n\tpath = "+slash(a)+"\n"), opts).Decode(cfg)

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
