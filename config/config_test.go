package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ConfigSuite struct {
	suite.Suite
}

func TestConfigSuite(t *testing.T) {
	suite.Run(t, new(ConfigSuite))
}

func (s *ConfigSuite) TestUnmarshal() {
	input := []byte(`[core]
		bare = true
		worktree = foo
		commentchar = bar
		autocrlf = true
[user]
		name = John Doe
		email = john@example.com
[author]
		name = Jane Roe
		email = jane@example.com
[committer]
		name = Richard Roe
		email = richard@example.com
[pack]
		window = 20
[remote "origin"]
		url = git@github.com:mcuadros/go-git.git
		fetch = +refs/heads/*:refs/remotes/origin/*
[remote "alt"]
		url = git@github.com:mcuadros/go-git.git
		url = git@github.com:src-d/go-git.git
		fetch = +refs/heads/*:refs/remotes/origin/*
		fetch = +refs/pull/*:refs/remotes/origin/pull/*
[remote "insteadOf"]
		url = https://github.com/kostyay/go-git.git
[remote "win-local"]
		url = X:\\Git\\
[submodule "qux"]
		path = qux
		url = https://github.com/foo/qux.git
		branch = bar
[branch "master"]
		remote = origin
		merge = refs/heads/master
		description = "Add support for branch description.\\n\\nEdit branch description: git branch --edit-description\\n"
[init]
		defaultBranch = main
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	s.True(cfg.Core.IsBare)
	s.Equal("foo", cfg.Core.Worktree)
	s.Equal("bar", cfg.Core.CommentChar)
	s.Equal("true", cfg.Core.AutoCRLF)
	s.Equal("John Doe", cfg.User.Name)
	s.Equal("john@example.com", cfg.User.Email)
	s.Equal("Jane Roe", cfg.Author.Name)
	s.Equal("jane@example.com", cfg.Author.Email)
	s.Equal("Richard Roe", cfg.Committer.Name)
	s.Equal("richard@example.com", cfg.Committer.Email)
	s.Equal(uint(20), cfg.Pack.Window)
	s.Len(cfg.Remotes, 4)
	s.Equal("origin", cfg.Remotes["origin"].Name)
	s.Equal([]string{"git@github.com:mcuadros/go-git.git"}, cfg.Remotes["origin"].URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, cfg.Remotes["origin"].Fetch)
	s.Equal("alt", cfg.Remotes["alt"].Name)
	s.Equal([]string{"git@github.com:mcuadros/go-git.git", "git@github.com:src-d/go-git.git"}, cfg.Remotes["alt"].URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*:refs/remotes/origin/pull/*"}, cfg.Remotes["alt"].Fetch)
	s.Equal("win-local", cfg.Remotes["win-local"].Name)
	s.Equal([]string{"X:\\Git\\"}, cfg.Remotes["win-local"].URLs)
	s.Equal([]string{"ssh://git@github.com/kostyay/go-git.git"}, cfg.Remotes["insteadOf"].URLs)
	s.Len(cfg.Submodules, 1)
	s.Equal("qux", cfg.Submodules["qux"].Name)
	s.Equal("https://github.com/foo/qux.git", cfg.Submodules["qux"].URL)
	s.Equal("bar", cfg.Submodules["qux"].Branch)
	s.Equal("origin", cfg.Branches["master"].Remote)
	s.Equal(plumbing.ReferenceName("refs/heads/master"), cfg.Branches["master"].Merge)
	s.Equal("Add support for branch description.\n\nEdit branch description: git branch --edit-description\n", cfg.Branches["master"].Description)
	s.Equal("main", cfg.Init.DefaultBranch)
}

func (s *ConfigSuite) TestMarshal() {
	output := []byte(`[core]
	bare = true
	worktree = bar
	autocrlf = true
[pack]
	window = 20
[remote "alt"]
	url = git@github.com:mcuadros/go-git.git
	url = git@github.com:src-d/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	fetch = +refs/pull/*:refs/remotes/origin/pull/*
[remote "insteadOf"]
	url = https://github.com/kostyay/go-git.git
[remote "origin"]
	url = git@github.com:mcuadros/go-git.git
[remote "win-local"]
	url = "X:\\Git\\"
[submodule "qux"]
	url = https://github.com/foo/qux.git
[branch "master"]
	remote = origin
	merge = refs/heads/master
	description = "Add support for branch description.\\n\\nEdit branch description: git branch --edit-description\\n"
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
[init]
	defaultBranch = main
`)

	cfg := NewConfig()
	cfg.Core.IsBare = true
	cfg.Core.Worktree = "bar"
	cfg.Core.AutoCRLF = "true"
	cfg.Pack.Window = 20
	cfg.Init.DefaultBranch = "main"
	cfg.Remotes["origin"] = &RemoteConfig{
		Name: "origin",
		URLs: []string{"git@github.com:mcuadros/go-git.git"},
	}

	cfg.Remotes["alt"] = &RemoteConfig{
		Name:  "alt",
		URLs:  []string{"git@github.com:mcuadros/go-git.git", "git@github.com:src-d/go-git.git"},
		Fetch: []RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*:refs/remotes/origin/pull/*"},
	}

	cfg.Remotes["win-local"] = &RemoteConfig{
		Name: "win-local",
		URLs: []string{"X:\\Git\\"},
	}

	cfg.Remotes["insteadOf"] = &RemoteConfig{
		Name: "insteadOf",
		URLs: []string{"https://github.com/kostyay/go-git.git"},
	}

	cfg.Submodules["qux"] = &Submodule{
		Name: "qux",
		URL:  "https://github.com/foo/qux.git",
	}

	cfg.Branches["master"] = &Branch{
		Name:        "master",
		Remote:      "origin",
		Merge:       "refs/heads/master",
		Description: "Add support for branch description.\n\nEdit branch description: git branch --edit-description\n",
	}

	cfg.URLs["ssh://git@github.com/"] = &URL{
		Name:       "ssh://git@github.com/",
		InsteadOfs: []string{"https://github.com/"},
	}

	b, err := cfg.Marshal()
	s.NoError(err)

	s.Equal(string(output), string(b))
}

func (s *ConfigSuite) TestUnmarshalMarshal() {
	input := []byte(`[core]
	bare = true
	worktree = foo
	custom = ignored
	autocrlf = true
[user]
	name = John Doe
	email = john@example.com
[author]
	name = Jane Roe
	email = jane@example.com
[committer]
	name = Richard Roe
	email = richard@example.co
[pack]
	window = 20
[remote "insteadOf"]
	url = https://github.com/kostyay/go-git.git
[remote "origin"]
	url = git@github.com:mcuadros/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	mirror = true
[remote "win-local"]
	url = "X:\\Git\\"
[branch "master"]
	remote = origin
	merge = refs/heads/master
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	output, err := cfg.Marshal()
	s.NoError(err)
	s.Equal(string(input), string(output))
}

func (s *ConfigSuite) TestLoadConfigXDG() {
	cfg := NewConfig()
	cfg.User.Name = "foo"
	cfg.User.Email = "foo@foo.com"

	tmp, err := util.TempDir(osfs.Default, "", "test-commit-options")
	s.NoError(err)
	defer util.RemoveAll(osfs.Default, tmp)

	err = osfs.Default.MkdirAll(filepath.Join(tmp, "git"), 0777)
	s.NoError(err)

	os.Setenv("XDG_CONFIG_HOME", tmp)
	defer func() {
		os.Setenv("XDG_CONFIG_HOME", "")
	}()

	content, err := cfg.Marshal()
	s.NoError(err)

	cfgFile := filepath.Join(tmp, "git/config")
	err = util.WriteFile(osfs.Default, cfgFile, content, 0777)
	s.NoError(err)

	cfg, err = LoadConfig(GlobalScope)
	s.NoError(err)

	s.Equal("foo@foo.com", cfg.User.Email)
}

func (s *ConfigSuite) TestLoadConfigGITConfigGlobalEnv() {
	cfg := NewConfig()
	cfg.User.Name = "env-global"
	cfg.User.Email = "env-global@example.com"

	tmp, err := util.TempDir(osfs.Default, "", "test-git-config-global")
	s.NoError(err)
	defer util.RemoveAll(osfs.Default, tmp)

	content, err := cfg.Marshal()
	s.NoError(err)

	cfgFile := filepath.Join(tmp, "globalgitconfig")
	err = util.WriteFile(osfs.Default, cfgFile, content, 0777)
	s.NoError(err)

	// Set GIT_CONFIG_GLOBAL to point to our temp config file (restored by t.Setenv)
	s.T().Setenv("GIT_CONFIG_GLOBAL", cfgFile)

	got, err := LoadConfig(GlobalScope)
	s.NoError(err)
	s.Equal("env-global@example.com", got.User.Email)
}

func (s *ConfigSuite) TestLoadConfigGITConfigSystemEnv() {
	cfg := NewConfig()
	cfg.User.Name = "env-system"
	cfg.User.Email = "env-system@example.com"

	tmp, err := util.TempDir(osfs.Default, "", "test-git-config-system")
	s.NoError(err)
	defer util.RemoveAll(osfs.Default, tmp)

	content, err := cfg.Marshal()
	s.NoError(err)

	cfgFile := filepath.Join(tmp, "systemgitconfig")
	err = util.WriteFile(osfs.Default, cfgFile, content, 0777)
	s.NoError(err)

	// Set GIT_CONFIG_SYSTEM to point to our temp config file (restored by t.Setenv)
	s.T().Setenv("GIT_CONFIG_SYSTEM", cfgFile)

	gotFiles, err := Paths(SystemScope)
	s.NoError(err)
	// Ensure our file is the first candidate
	s.True(len(gotFiles) > 0)
	s.Equal(cfgFile, gotFiles[0])

	got, err := LoadConfig(SystemScope)
	s.NoError(err)
	s.Equal("env-system@example.com", got.User.Email)
}

func (s *ConfigSuite) TestLoadConfigGlobalPrecedence() {
	// Prepare three different configs: env (GIT_CONFIG_GLOBAL), xdg, and home
	envCfg := NewConfig()
	envCfg.User.Email = "env@example.com"

	xdgCfg := NewConfig()
	xdgCfg.User.Email = "xdg@example.com"

	homeCfg := NewConfig()
	homeCfg.User.Email = "home@example.com"

	// Create temporary dirs/files
	tmp, err := util.TempDir(osfs.Default, "", "test-global-precedence")
	s.NoError(err)
	defer util.RemoveAll(osfs.Default, tmp)

	// env file
	envFile := filepath.Join(tmp, "envgitconfig")
	b, err := envCfg.Marshal()
	s.NoError(err)
	s.NoError(util.WriteFile(osfs.Default, envFile, b, 0777))

	// xdg file
	xdgDir := filepath.Join(tmp, "xdg")
	s.NoError(os.MkdirAll(xdgDir, 0777))
	xdgFile := filepath.Join(xdgDir, "git", "config")
	s.NoError(os.MkdirAll(filepath.Dir(xdgFile), 0777))
	b, err = xdgCfg.Marshal()
	s.NoError(err)
	s.NoError(util.WriteFile(osfs.Default, xdgFile, b, 0777))

	// home .gitconfig
	homeDir := filepath.Join(tmp, "home")
	s.NoError(os.MkdirAll(homeDir, 0777))
	homeFile := filepath.Join(homeDir, ".gitconfig")
	b, err = homeCfg.Marshal()
	s.NoError(err)
	s.NoError(util.WriteFile(osfs.Default, homeFile, b, 0777))

	// Use testing.T.Setenv so the environment is changed for the test and
	// automatically restored when it finishes.
	s.T().Setenv("GIT_CONFIG_GLOBAL", envFile)
	s.T().Setenv("XDG_CONFIG_HOME", xdgDir)
	s.T().Setenv("HOME", homeDir)

	got, err := LoadConfig(GlobalScope)
	s.NoError(err)
	s.Equal("env@example.com", got.User.Email)

	// Case 2: unset GIT_CONFIG_GLOBAL, XDG exists -> should pick xdg
	s.T().Setenv("GIT_CONFIG_GLOBAL", "")
	got, err = LoadConfig(GlobalScope)
	s.NoError(err)
	s.Equal("xdg@example.com", got.User.Email)

	// Case 3: unset XDG, home .gitconfig exists -> should pick home
	s.T().Setenv("XDG_CONFIG_HOME", "")
	got, err = LoadConfig(GlobalScope)
	s.NoError(err)
	s.Equal("home@example.com", got.User.Email)
}

func (s *ConfigSuite) TestValidateConfig() {
	config := &Config{
		Remotes: map[string]*RemoteConfig{
			"bar": {
				Name: "bar",
				URLs: []string{"http://foo/bar"},
			},
		},
		Branches: map[string]*Branch{
			"bar": {
				Name: "bar",
			},
			"foo": {
				Name:   "foo",
				Remote: "origin",
				Merge:  plumbing.ReferenceName("refs/heads/foo"),
			},
		},
	}

	s.NoError(config.Validate())
}

func (s *ConfigSuite) TestValidateInvalidRemote() {
	config := &Config{
		Remotes: map[string]*RemoteConfig{
			"foo": {Name: "foo"},
		},
	}

	s.ErrorIs(config.Validate(), ErrRemoteConfigEmptyURL)
}

func (s *ConfigSuite) TestValidateInvalidRemoteKey() {
	config := &Config{
		Remotes: map[string]*RemoteConfig{
			"bar": {Name: "foo"},
		},
	}

	s.ErrorIs(config.Validate(), ErrInvalid)
}

func (s *ConfigSuite) TestRemoteConfigValidateMissingURL() {
	config := &RemoteConfig{Name: "foo"}
	s.ErrorIs(config.Validate(), ErrRemoteConfigEmptyURL)
}

func (s *ConfigSuite) TestRemoteConfigValidateMissingName() {
	config := &RemoteConfig{}
	s.ErrorIs(config.Validate(), ErrRemoteConfigEmptyName)
}

func (s *ConfigSuite) TestRemoteConfigValidateDefault() {
	config := &RemoteConfig{Name: "foo", URLs: []string{"http://foo/bar"}}
	s.NoError(config.Validate())

	fetch := config.Fetch
	s.Len(fetch, 1)
	s.Equal("+refs/heads/*:refs/remotes/foo/*", fetch[0].String())
}

func (s *ConfigSuite) TestValidateInvalidBranchKey() {
	config := &Config{
		Branches: map[string]*Branch{
			"foo": {
				Name:   "bar",
				Remote: "origin",
				Merge:  plumbing.ReferenceName("refs/heads/bar"),
			},
		},
	}

	s.ErrorIs(config.Validate(), ErrInvalid)
}

func (s *ConfigSuite) TestValidateInvalidBranch() {
	config := &Config{
		Branches: map[string]*Branch{
			"bar": {
				Name:   "bar",
				Remote: "origin",
				Merge:  plumbing.ReferenceName("refs/heads/bar"),
			},
			"foo": {
				Name:   "foo",
				Remote: "origin",
				Merge:  plumbing.ReferenceName("baz"),
			},
		},
	}

	s.ErrorIs(config.Validate(), errBranchInvalidMerge)
}

func (s *ConfigSuite) TestRemoteConfigDefaultValues() {
	config := NewConfig()

	s.Len(config.Remotes, 0)
	s.Len(config.Branches, 0)
	s.Len(config.Submodules, 0)
	s.NotNil(config.Raw)
	s.Equal(DefaultPackWindow, config.Pack.Window)
}

func (s *ConfigSuite) TestLoadConfigLocalScope() {
	cfg, err := LoadConfig(LocalScope)
	s.NotNil(err)
	s.Nil(cfg)
}

func (s *ConfigSuite) TestRemoveUrlOptions() {
	buf := []byte(`
[remote "alt"]
	url = git@github.com:mcuadros/go-git.git
	url = git@github.com:src-d/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	fetch = +refs/pull/*:refs/remotes/origin/pull/*`)

	cfg := NewConfig()
	err := cfg.Unmarshal(buf)
	s.NoError(err)
	s.Len(cfg.Remotes, 1)
	cfg.Remotes["alt"].URLs = []string{}

	buf, err = cfg.Marshal()
	s.NoError(err)
	if strings.Contains(string(buf), "url") {
		s.Fail("config should not contain any url sections")
	}
	s.NoError(err)
}

func (s *ConfigSuite) TestProtocol() {
	buf := []byte(`
[protocol]
	version = 1`)

	cfg := NewConfig()
	err := cfg.Unmarshal(buf)
	s.NoError(err)
	s.Equal(protocol.V1, cfg.Protocol.Version)

	cfg.Protocol.Version = protocol.V2
	buf, err = cfg.Marshal()
	s.NoError(err)

	if !strings.Contains(string(buf), "version = 2") {
		s.Fail("marshal did not update version")
	}
	s.NoError(err)
}

func (s *ConfigSuite) TestUnmarshalRemotes() {
	input := []byte(`[core]
	bare = true
	worktree = foo
	custom = ignored
[user]
	name = John Doe
	email = john@example.com
[remote "origin"]
	url = https://git.sr.ht/~mcepl/go-git
	pushurl = git@git.sr.ht:~mcepl/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	mirror = true
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	s.Equal("https://git.sr.ht/~mcepl/go-git", cfg.Remotes["origin"].URLs[0])
	s.Equal("git@git.sr.ht:~mcepl/go-git.git", cfg.Remotes["origin"].URLs[1])
}

func (s *ConfigSuite) TestUnmarshalRemotesUnnamedFirst() {
	input := []byte(`
[remote ""]
  url = https://github.com/CLBRITTON2/go-git.git
  fetch = +refs/heads/*:refs/remotes/origin/*
[remote "upstream"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
	`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	unnamedRemote, ok := cfg.Remotes[""]
	s.True(ok, "Expected unnamed remote to be present")
	s.Equal([]string{"https://github.com/CLBRITTON2/go-git.git"}, unnamedRemote.URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, unnamedRemote.Fetch)

	namedRemote, ok := cfg.Remotes["upstream"]
	s.True(ok, "Expected named remote 'upstream' to be present")
	s.Equal([]string{"https://github.com/go-git/go-git.git"}, namedRemote.URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/upstream/*"}, namedRemote.Fetch)
}

func (s *ConfigSuite) TestUnmarshalRemotesNamedFirst() {
	input := []byte(`
[remote "upstream"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
[remote ""]
  url = https://github.com/CLBRITTON2/go-git.git
  fetch = +refs/heads/*:refs/remotes/origin/*
	`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	namedRemote, ok := cfg.Remotes["upstream"]
	s.True(ok, "Expected a named remote 'upstream' to be present")
	s.Equal([]string{"https://github.com/go-git/go-git.git"}, namedRemote.URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/upstream/*"}, namedRemote.Fetch)

	unnamedRemote, ok := cfg.Remotes[""]
	s.True(ok, "Expected an unnamed remote to be present")
	s.Equal([]string{"https://github.com/CLBRITTON2/go-git.git"}, unnamedRemote.URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, unnamedRemote.Fetch)
}

func TestMerge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []*Config
		want  Config
	}{
		{
			name:  "nil",
			input: nil,
			want:  Config{},
		},
		{
			name: "separate objs",
			input: []*Config{
				{User: struct {
					Name  string
					Email string
				}{
					Name: "foo", Email: "bar@test",
				}},
				{
					Extensions: struct{ ObjectFormat config.ObjectFormat }{
						ObjectFormat: config.SHA256,
					},
				},
			},
			want: Config{
				User: struct {
					Name  string
					Email string
				}{
					Name:  "foo",
					Email: "bar@test",
				},
				Extensions: struct{ ObjectFormat config.ObjectFormat }{
					ObjectFormat: config.SHA256,
				},
			},
		},
		{
			name: "merge nested fields",
			input: []*Config{
				{User: struct {
					Name  string
					Email string
				}{Name: "foo"}},
				{User: struct {
					Name  string
					Email string
				}{Email: "bar@test"}},
			},
			want: Config{
				User: struct {
					Name  string
					Email string
				}{
					Name:  "foo",
					Email: "bar@test",
				},
			},
		},
		{
			name: "override nested fields",
			input: []*Config{
				{User: struct {
					Name  string
					Email string
				}{Name: "foo"}},
				{User: struct {
					Name  string
					Email string
				}{Name: "bar", Email: "foo@test"}},
			},
			want: Config{
				User: struct {
					Name  string
					Email string
				}{
					Name:  "bar",
					Email: "foo@test",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Merge(tc.input...)

			assert.Equal(t, tc.want, got)
		})
	}
}
