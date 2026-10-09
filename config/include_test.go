package config

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

func testRoot(parts ...string) string {
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

// loadFiles returns a load function for LoadWithIncludes that reads the
// given files from fs, and counts the passes.
func loadFiles(fs billy.Filesystem, passes *[]IncludeContext, paths ...string) func(IncludeContext) ([]*Config, error) {
	return func(ctx IncludeContext) ([]*Config, error) {
		*passes = append(*passes, ctx)
		ctx.FS = fs

		var cfgs []*Config
		for _, p := range paths {
			b, err := util.ReadFile(fs, p)
			if err != nil {
				return nil, err
			}
			cfg := NewConfig()
			if err := cfg.UnmarshalWithIncludes(b, ctx.FormatOptions(p)); err != nil {
				return nil, err
			}
			cfgs = append(cfgs, cfg)
		}
		return cfgs, nil
	}
}

func writeFiles(t *testing.T, files map[string]string) billy.Filesystem {
	t.Helper()
	fs := memfs.New()
	for p, content := range files {
		require.NoError(t, util.WriteFile(fs, p, []byte(content), 0o644))
	}
	return fs
}

func TestLoadWithIncludesReadsOnceWithoutRemoteURLConditions(t *testing.T) {
	t.Parallel()

	global := testRoot("global")
	fs := writeFiles(t, map[string]string{
		global: "[user]\n\tname = Global\n[remote \"origin\"]\n\turl = https://example.com/r.git\n",
	})

	var passes []IncludeContext
	cfgs, err := LoadWithIncludes(IncludeContext{}, loadFiles(fs, &passes, global))
	require.NoError(t, err)
	assert.Len(t, passes, 1)
	require.Len(t, cfgs, 1)
	assert.Equal(t, "Global", cfgs[0].User.Name)
}

// A hasconfig:remote.*.url: condition matches the remotes of every scope
// LoadWithIncludes reads, including one defined after the condition and
// one defined through a plain include.
func TestLoadWithIncludesResolvesRemoteURLConditions(t *testing.T) {
	t.Parallel()

	global, local := testRoot("global"), testRoot("repo", "config")
	fs := writeFiles(t, map[string]string{
		global: "[includeIf \"hasconfig:remote.*.url:https://work.example.com/**\"]\n" +
			"\tpath = " + filepath.ToSlash(testRoot("work")) + "\n",
		testRoot("work"):            "[user]\n\temail = me@work.example.com\n",
		local:                       "[include]\n\tpath = remotes\n",
		testRoot("repo", "remotes"): "[remote \"origin\"]\n\turl = https://work.example.com/r.git\n",
	})

	var passes []IncludeContext
	cfgs, err := LoadWithIncludes(IncludeContext{}, loadFiles(fs, &passes, global, local))
	require.NoError(t, err)
	require.Len(t, passes, 3)
	assert.False(t, passes[0].UnconditionalRemoteURL)
	assert.True(t, passes[1].UnconditionalRemoteURL)
	assert.Equal(t, []string{"https://work.example.com/r.git"}, passes[2].RemoteURLs)
	assert.Equal(t, "me@work.example.com", cfgs[0].User.Email)
}

// A file an includeIf reaches may not define the remotes a
// hasconfig:remote.*.url: condition matches, as git refuses those.
func TestLoadWithIncludesRefusesRemoteURLsInConditionalIncludes(t *testing.T) {
	t.Parallel()

	global := testRoot("global")
	fs := writeFiles(t, map[string]string{
		global: "[includeIf \"hasconfig:remote.*.url:https://work.example.com/**\"]\n" +
			"\tpath = " + filepath.ToSlash(testRoot("work")) + "\n",
		testRoot("work"): "[remote \"origin\"]\n\turl = https://work.example.com/r.git\n",
	})

	var passes []IncludeContext
	_, err := LoadWithIncludes(IncludeContext{}, loadFiles(fs, &passes, global))
	assert.ErrorIs(t, err, format.ErrRemoteURLInConditionalInclude)
}

func TestRemoteURLs(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	require.NoError(t, cfg.Unmarshal([]byte(`
[remote "origin"]
	url = git@github.com:work/a.git
	url = git@github.com:work/mirror.git
[remote "fork"]
	url = git@github.com:me/a.git
[remote ""]
	url = git@github.com:unnamed/a.git
`)))

	assert.ElementsMatch(t, []string{
		"git@github.com:work/a.git",
		"git@github.com:work/mirror.git",
		"git@github.com:me/a.git",
		"git@github.com:unnamed/a.git",
	}, remoteURLs([]*Config{cfg, nil}))
}

// Looking for remotes and conditions leaves the config as it was, rather
// than adding the empty sections Config.Section would.
func TestRemoteURLLookupsDoNotAddSections(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	require.NoError(t, cfg.Unmarshal([]byte("[user]\n\tname = A\n")))
	sections := len(cfg.Raw.Sections)

	assert.Empty(t, remoteURLs([]*Config{cfg}))
	assert.False(t, hasRemoteURLCondition([]*Config{cfg}))
	assert.False(t, cfg.Raw.HasSection(includeIfSection))
	assert.Len(t, cfg.Raw.Sections, sections)
}
