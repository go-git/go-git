package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/protocol"
)

type ConfigSuite struct {
	suite.Suite
}

func TestConfigSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ConfigSuite))
}

func configWith(setup func(*Config)) *Config {
	c := &Config{Raw: config.New()}
	if setup != nil {
		setup(c)
	}
	return c
}

func configValueWith(setup func(*Config)) Config {
	return *configWith(setup)
}

func (s *ConfigSuite) TestUnmarshal() {
	input := []byte(`[core]
		bare = true
		worktree = foo
		commentchar = bar
		autocrlf = true
		filemode = false
		hooksPath = custom-hooks
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

	s.True(cfg.Core().IsBare)
	s.Equal("foo", cfg.Core().Worktree)
	s.Equal("bar", cfg.Core().CommentChar)
	s.Equal("true", cfg.Core().AutoCRLF)
	s.False(cfg.Core().FileMode)
	s.Equal("custom-hooks", cfg.Core().HooksPath)
	s.Equal("John Doe", cfg.User().Name)
	s.Equal("john@example.com", cfg.User().Email)
	s.Equal("Jane Roe", cfg.Author().Name)
	s.Equal("jane@example.com", cfg.Author().Email)
	s.Equal("Richard Roe", cfg.Committer().Name)
	s.Equal("richard@example.com", cfg.Committer().Email)
	s.Equal(uint(20), cfg.Pack().Window)
	s.Len(cfg.Remotes(), 4)
	s.Equal("origin", cfg.Remotes()["origin"].Name)
	s.Equal([]string{"git@github.com:mcuadros/go-git.git"}, cfg.Remotes()["origin"].URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, cfg.Remotes()["origin"].Fetch)
	s.Equal("alt", cfg.Remotes()["alt"].Name)
	s.Equal([]string{"git@github.com:mcuadros/go-git.git", "git@github.com:src-d/go-git.git"}, cfg.Remotes()["alt"].URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*:refs/remotes/origin/pull/*"}, cfg.Remotes()["alt"].Fetch)
	s.Equal("win-local", cfg.Remotes()["win-local"].Name)
	s.Equal([]string{"X:\\Git\\"}, cfg.Remotes()["win-local"].URLs)
	s.Equal([]string{"ssh://git@github.com/kostyay/go-git.git"}, cfg.Remotes()["insteadOf"].URLs)
	s.Len(cfg.Submodules(), 1)
	s.Equal("qux", cfg.Submodules()["qux"].Name)
	s.Equal("https://github.com/foo/qux.git", cfg.Submodules()["qux"].URL)
	s.Equal("bar", cfg.Submodules()["qux"].Branch)
	s.Equal("origin", cfg.Branches()["master"].Remote)
	s.Equal(plumbing.ReferenceName("refs/heads/master"), cfg.Branches()["master"].Merge)
	s.Equal("Add support for branch description.\n\nEdit branch description: git branch --edit-description\n", cfg.Branches()["master"].Description)
	s.Equal("main", cfg.Init().DefaultBranch)
}

func (s *ConfigSuite) TestMarshal() {
	output := []byte(`[core]
	filemode = true
	bare = true
	worktree = bar
	autocrlf = true
	hooksPath = custom-hooks
[pack]
	window = 20
[init]
	defaultBranch = main
[remote "origin"]
	url = git@github.com:mcuadros/go-git.git
[remote "alt"]
	url = git@github.com:mcuadros/go-git.git
	url = git@github.com:src-d/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	fetch = +refs/pull/*:refs/remotes/origin/pull/*
[remote "win-local"]
	url = "X:\\Git\\"
[remote "insteadOf"]
	url = https://github.com/kostyay/go-git.git
[submodule "qux"]
	url = https://github.com/foo/qux.git
[branch "master"]
	remote = origin
	merge = refs/heads/master
	description = "Add support for branch description.\\n\\nEdit branch description: git branch --edit-description\\n"
[url "ssh://git@github.com/"]
	insteadOf = https://github.com/
`)

	cfg := NewConfig()
	cfg.SetBare(true)
	cfg.SetWorktree("bar")
	cfg.SetAutoCRLF("true")
	cfg.SetHooksPath("custom-hooks")
	cfg.SetPackWindow(20)
	cfg.SetInitDefaultBranch("main")
	cfg.SetRemote(&RemoteConfig{
		Name: "origin",
		URLs: []string{"git@github.com:mcuadros/go-git.git"},
	})

	cfg.SetRemote(&RemoteConfig{
		Name:  "alt",
		URLs:  []string{"git@github.com:mcuadros/go-git.git", "git@github.com:src-d/go-git.git"},
		Fetch: []RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*:refs/remotes/origin/pull/*"},
	})

	cfg.SetRemote(&RemoteConfig{
		Name: "win-local",
		URLs: []string{"X:\\Git\\"},
	})

	cfg.SetRemote(&RemoteConfig{
		Name: "insteadOf",
		URLs: []string{"https://github.com/kostyay/go-git.git"},
	})

	cfg.SetSubmodule(&Submodule{
		Name: "qux",
		URL:  "https://github.com/foo/qux.git",
	})

	cfg.SetBranch(&Branch{
		Name:        "master",
		Remote:      "origin",
		Merge:       "refs/heads/master",
		Description: "Add support for branch description.\n\nEdit branch description: git branch --edit-description\n",
	})

	cfg.AddURL(&URL{
		Name:       "ssh://git@github.com/",
		InsteadOfs: []string{"https://github.com/"},
	})

	b, err := cfg.Marshal()
	s.NoError(err)

	s.Equal(string(output), string(b))
}

func TestUnmarshalMarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
	}{
		{
			`[core]
	bare = true
	worktree = foo
	custom = ignored
	autocrlf = true
	filemode = true
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
`,
		},
		{
			`[core]
	repositoryformatversion = 1
	bare = false
	filemode = true
[branch "main"]
	remote = origin
	merge = refs/heads/main
	rebase = true
[extensions]
	objectformat = sha256
`,
		},
		{
			`[core]
	repositoryformatversion = 1
	bare = false
	filemode = true
[branch "main"]
	remote = origin
	merge = refs/heads/main
	rebase = true
[extensions]
	objectformat = sha1
`,
		},
	}

	for _, tc := range tests {
		cfg := NewConfig()
		err := cfg.Unmarshal([]byte(tc.input))
		require.NoError(t, err)

		output, err := cfg.Marshal()
		require.NoError(t, err)
		assert.Equal(t, string(tc.input), string(output))
	}
}

func TestMarshalExtensions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setup       func(*Config)
		wantSection bool
	}{
		{
			name:        "no extensions set omits section",
			setup:       func(_ *Config) {},
			wantSection: false,
		},
		{
			name: "WorktreeConfig true writes section",
			setup: func(c *Config) {
				c.SetRepositoryFormatVersion(config.Version1)
				c.SetWorktreeConfig(true)
			},
			wantSection: true,
		},
		{
			name: "ObjectFormat set writes section",
			setup: func(c *Config) {
				c.SetRepositoryFormatVersion(config.Version1)
				c.SetObjectFormat(config.SHA256)
			},
			wantSection: true,
		},
		{
			name: "extensions written regardless of repository format version",
			setup: func(c *Config) {
				c.SetRepositoryFormatVersion(config.Version0)
				c.SetObjectFormat(config.SHA256)
				c.SetWorktreeConfig(true)
			},
			wantSection: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewConfig()
			tc.setup(cfg)

			out, err := cfg.Marshal()
			require.NoError(t, err)

			hasSection := strings.Contains(string(out), "[extensions]")
			assert.Equal(t, tc.wantSection, hasSection)
		})
	}
}

func (s *ConfigSuite) TestLoadConfigXDG() {
	cfg := NewConfig()
	cfg.SetUser(User{Name: "foo", Email: "foo@foo.com"})

	tmp := s.T().TempDir()

	err := os.MkdirAll(filepath.Join(tmp, "git"), 0o777)
	s.NoError(err)

	os.Setenv("XDG_CONFIG_HOME", tmp)
	defer func() {
		os.Setenv("XDG_CONFIG_HOME", "")
	}()

	content, err := cfg.Marshal()
	s.NoError(err)

	cfgFile := filepath.Join(tmp, "git/config")
	err = os.WriteFile(cfgFile, content, 0o777)
	s.NoError(err)

	cfg, err = LoadConfig(GlobalScope)
	s.NoError(err)

	s.Equal("foo@foo.com", cfg.User().Email)
}

func (s *ConfigSuite) TestValidateConfig() {
	config := configWith(func(c *Config) {
		c.SetRemote(&RemoteConfig{
			Name: "bar",
			URLs: []string{"http://foo/bar"},
		})
		c.SetBranch(&Branch{
			Name: "bar",
		})
		c.SetBranch(&Branch{
			Name:   "foo",
			Remote: "origin",
			Merge:  plumbing.ReferenceName("refs/heads/foo"),
		})
	})

	s.NoError(config.Validate())
}

func (s *ConfigSuite) TestValidateInvalidRemote() {
	config := configWith(func(c *Config) {
		c.SetRemote(&RemoteConfig{Name: "foo"})
	})

	s.ErrorIs(config.Validate(), ErrRemoteConfigEmptyURL)
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

func (s *ConfigSuite) TestValidateBranchNonRefsPrefix() {
	// Real git allows any value for branch.*.merge, including values
	// without a refs/ prefix. Validate should accept them.
	config := configWith(func(c *Config) {
		c.SetBranch(&Branch{
			Name:   "bar",
			Remote: "origin",
			Merge:  plumbing.ReferenceName("refs/heads/bar"),
		})
		c.SetBranch(&Branch{
			Name:   "foo",
			Remote: "origin",
			Merge:  plumbing.ReferenceName("baz"),
		})
	})

	s.NoError(config.Validate())
}

func (s *ConfigSuite) TestRemoteConfigDefaultValues() {
	config := NewConfig()

	s.Len(config.Remotes(), 0)
	s.Len(config.Branches(), 0)
	s.Len(config.Submodules(), 0)
	s.NotNil(config.Raw)
	s.Equal(DefaultPackWindow, config.Pack().Window)
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
	s.Len(cfg.Remotes(), 1)
	alt := cfg.Remote("alt")
	alt.URLs = []string{}
	cfg.SetRemote(alt)

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
	s.Equal(protocol.V1, cfg.Protocol().Version)

	cfg.SetProtocolVersion(protocol.V2)
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

	s.Equal("https://git.sr.ht/~mcepl/go-git", cfg.Remotes()["origin"].URLs[0])
	s.Equal("git@git.sr.ht:~mcepl/go-git.git", cfg.Remotes()["origin"].URLs[1])
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

	unnamedRemote, ok := cfg.Remotes()[""]
	s.True(ok, "Expected unnamed remote to be present")
	s.Equal([]string{"https://github.com/CLBRITTON2/go-git.git"}, unnamedRemote.URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, unnamedRemote.Fetch)

	namedRemote, ok := cfg.Remotes()["upstream"]
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

	namedRemote, ok := cfg.Remotes()["upstream"]
	s.True(ok, "Expected a named remote 'upstream' to be present")
	s.Equal([]string{"https://github.com/go-git/go-git.git"}, namedRemote.URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/upstream/*"}, namedRemote.Fetch)

	unnamedRemote, ok := cfg.Remotes()[""]
	s.True(ok, "Expected an unnamed remote to be present")
	s.Equal([]string{"https://github.com/CLBRITTON2/go-git.git"}, unnamedRemote.URLs)
	s.Equal([]RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, unnamedRemote.Fetch)
}

func TestUnmarshalPackReverseIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantRead  bool
		wantWrite bool
	}{
		{
			name:      "both true",
			input:     "[pack]\n\treadReverseIndex = true\n\twriteReverseIndex = true\n",
			wantRead:  true,
			wantWrite: true,
		},
		{
			name:      "both false",
			input:     "[pack]\n\treadReverseIndex = false\n\twriteReverseIndex = false\n",
			wantRead:  false,
			wantWrite: false,
		},
		{
			name:      "only readReverseIndex false",
			input:     "[pack]\n\treadReverseIndex = false\n",
			wantRead:  false,
			wantWrite: true,
		},
		{
			name:      "only writeReverseIndex false",
			input:     "[pack]\n\twriteReverseIndex = false\n",
			wantRead:  true,
			wantWrite: false,
		},
		{
			name:      "absent defaults to true",
			input:     "[pack]\n\twindow = 10\n",
			wantRead:  true,
			wantWrite: true,
		},
		{
			name:      "empty pack section defaults to true",
			input:     "[pack]\n",
			wantRead:  true,
			wantWrite: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewConfig()
			err := cfg.Unmarshal([]byte(tc.input))
			require.NoError(t, err)

			assert.Equal(t, tc.wantRead, cfg.Pack().ReadReverseIndex)
			assert.Equal(t, tc.wantWrite, cfg.Pack().WriteReverseIndex)
		})
	}
}

func TestMarshalPackReverseIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		read           bool
		write          bool
		wantReadFalse  bool
		wantWriteFalse bool
	}{
		{
			name:           "both true omits keys",
			read:           true,
			write:          true,
			wantReadFalse:  false,
			wantWriteFalse: false,
		},
		{
			name:           "both false writes keys",
			read:           false,
			write:          false,
			wantReadFalse:  true,
			wantWriteFalse: true,
		},
		{
			name:           "only readReverseIndex false",
			read:           false,
			write:          true,
			wantReadFalse:  true,
			wantWriteFalse: false,
		},
		{
			name:           "only writeReverseIndex false",
			read:           true,
			write:          false,
			wantReadFalse:  false,
			wantWriteFalse: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewConfig()
			cfg.SetPackReadReverseIndex(tc.read)
			cfg.SetPackWriteReverseIndex(tc.write)

			b, err := cfg.Marshal()
			require.NoError(t, err)
			output := string(b)

			assert.Equal(t, tc.wantReadFalse, strings.Contains(output, "readReverseIndex = false"), "readReverseIndex = false presence")
			assert.Equal(t, tc.wantWriteFalse, strings.Contains(output, "writeReverseIndex = false"), "writeReverseIndex = false presence")
		})
	}
}

func TestUnmarshalMarshalPackReverseIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name: "both false",
			input: "[core]\n\tbare = false\n\tfilemode = true\n" +
				"[pack]\n\treadReverseIndex = false\n\twriteReverseIndex = false\n",
		},
		{
			name: "with window and both false",
			input: "[core]\n\tbare = false\n\tfilemode = true\n" +
				"[pack]\n\twindow = 20\n\treadReverseIndex = false\n\twriteReverseIndex = false\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewConfig()
			err := cfg.Unmarshal([]byte(tc.input))
			require.NoError(t, err)

			output, err := cfg.Marshal()
			require.NoError(t, err)

			assert.Equal(t, tc.input, string(output))
		})
	}
}

func TestUnmarshalIndexSkipHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  OptBool
	}{
		{
			name:  "true",
			input: "[index]\n\tskipHash = true\n",
			want:  OptBoolTrue,
		},
		{
			name:  "false",
			input: "[index]\n\tskipHash = false\n",
			want:  OptBoolFalse,
		},
		{
			name:  "absent defaults to unset",
			input: "[core]\n\tbare = false\n",
			want:  OptBoolUnset,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewConfig()
			err := cfg.Unmarshal([]byte(tc.input))
			require.NoError(t, err)

			assert.Equal(t, tc.want, cfg.Index().SkipHash)
		})
	}
}

func TestMarshalIndexSkipHash(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	cfg.SetIndexSkipHash(OptBoolTrue)

	b, err := cfg.Marshal()
	require.NoError(t, err)
	assert.Contains(t, string(b), "skipHash = true")

	// Round-trip: unmarshal the marshaled output and verify.
	cfg2 := NewConfig()
	err = cfg2.Unmarshal(b)
	require.NoError(t, err)
	assert.Equal(t, OptBoolTrue, cfg2.Index().SkipHash)
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
				configWith(func(c *Config) {
					c.SetUser(User{Name: "foo", Email: "bar@test"})
				}),
				configWith(func(c *Config) {
					c.SetObjectFormat(config.SHA256)
					c.SetWorktreeConfig(true)
				}),
			},
			want: configValueWith(func(c *Config) {
				c.SetUser(User{Name: "foo", Email: "bar@test"})
				c.SetObjectFormat(config.SHA256)
				c.SetWorktreeConfig(true)
			}),
		},
		{
			name: "merge nested fields",
			input: []*Config{
				configWith(func(c *Config) {
					c.SetUser(User{Name: "foo"})
				}),
				configWith(func(c *Config) {
					c.SetUser(User{Email: "bar@test"})
				}),
			},
			want: configValueWith(func(c *Config) {
				c.SetUser(User{Name: "foo", Email: "bar@test"})
			}),
		},
		{
			name: "override nested fields",
			input: []*Config{
				configWith(func(c *Config) {
					c.SetUser(User{Name: "foo"})
				}),
				configWith(func(c *Config) {
					c.SetUser(User{Name: "bar", Email: "foo@test"})
				}),
			},
			want: configValueWith(func(c *Config) {
				c.SetUser(User{Name: "bar", Email: "foo@test"})
			}),
		},
		{
			name: "src nil map preserves dst map",
			input: []*Config{
				configWith(func(c *Config) {
					c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://example.com/repo.git"}})
				}),
				configWith(func(c *Config) {
					// Remotes is nil (zero value)
					c.SetBranch(&Branch{Name: "main"})
				}),
			},
			want: configValueWith(func(c *Config) {
				c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://example.com/repo.git"}})
				c.SetBranch(&Branch{Name: "main"})
			}),
		},
		{
			name: "src empty map preserves dst map",
			input: []*Config{
				configWith(func(c *Config) {
					c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://example.com/repo.git"}})
				}),
				configWith(func(_ *Config) {
					// Remotes is explicitly initialised but empty (mirrors NewConfig behaviour).
				}),
			},
			want: configValueWith(func(c *Config) {
				c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://example.com/repo.git"}})
			}),
		},
		{
			name: "merge maps with disjoint keys",
			input: []*Config{
				configWith(func(c *Config) {
					c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://example.com/repo.git"}})
				}),
				configWith(func(c *Config) {
					c.SetRemote(&RemoteConfig{Name: "upstream", URLs: []string{"https://upstream.com/repo.git"}})
				}),
			},
			want: configValueWith(func(c *Config) {
				c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://example.com/repo.git"}})
				c.SetRemote(&RemoteConfig{Name: "upstream", URLs: []string{"https://upstream.com/repo.git"}})
			}),
		},
		{
			name: "src map entry overrides dst map entry",
			input: []*Config{
				configWith(func(c *Config) {
					c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://old.com/repo.git"}})
				}),
				configWith(func(c *Config) {
					c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://new.com/repo.git"}})
				}),
			},
			want: configValueWith(func(c *Config) {
				c.SetRemote(&RemoteConfig{Name: "origin", URLs: []string{"https://new.com/repo.git"}})
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Merge(tc.input...)

			gotBytes, err := got.Marshal()
			require.NoError(t, err)
			want := tc.want
			wantBytes, err := want.Marshal()
			require.NoError(t, err)

			assert.Equal(t, string(wantBytes), string(gotBytes))
		})
	}

	t.Run("merge resets Raw section", func(t *testing.T) {
		t.Parallel()

		const baseConfig = "[core]\n\tbare = false\n\tfilemode = true\n" +
			"repositoryformatversion = 1\n" +
			"[extensions]\n\tworktreeConfig = true\n" +
			"[user]\n\tname = base-user\n\temail = base@example.com\n"
		const wtConfig = "[user]\n\tname = wt-user\n"

		base, err := ReadConfig(strings.NewReader(baseConfig))
		require.NoError(t, err)
		wt, err := ReadConfig(strings.NewReader(wtConfig))
		require.NoError(t, err)

		merged := Merge(base, wt)

		assert.Equal(t, "wt-user", merged.User().Name)
		assert.Equal(t, "base@example.com", merged.User().Email)
		assert.True(t, merged.Extensions().WorktreeConfig)

		assert.True(t, merged.Raw.HasSection("extensions"),
			"[extensions] section was dropped from merged Raw")
		assert.Equal(t, "true",
			merged.Raw.Section("extensions").Options.Get("worktreeConfig"))

		assert.Equal(t, "wt-user",
			merged.Raw.Section("user").Options.Get("name"))
	})
}

func TestGPGConfig(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal gpg format", func(t *testing.T) {
		t.Parallel()
		input := []byte(`[gpg]
	format = ssh
`)
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, "ssh", cfg.GPG().Format)
	})

	t.Run("unmarshal gpg.ssh.allowedSignersFile", func(t *testing.T) {
		t.Parallel()
		input := []byte(`[gpg "ssh"]
	allowedSignersFile = ~/.ssh/allowed_signers
`)
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, "~/.ssh/allowed_signers", cfg.GPG().SSH.AllowedSignersFile)
	})

	t.Run("unmarshal gpg format and ssh subsection", func(t *testing.T) {
		t.Parallel()
		input := []byte(`[gpg]
	format = ssh
[gpg "ssh"]
	allowedSignersFile = /home/user/.ssh/allowed_signers
`)
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, "ssh", cfg.GPG().Format)
		assert.Equal(t, "/home/user/.ssh/allowed_signers", cfg.GPG().SSH.AllowedSignersFile)
	})

	t.Run("marshal gpg format", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.SetGPGFormat("ssh")

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), "[gpg]")
		assert.Contains(t, string(data), "format = ssh")
	})

	t.Run("marshal gpg.ssh.allowedSignersFile", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.SetGPGSSHAllowedSignersFile("~/.ssh/allowed_signers")

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), `[gpg "ssh"]`)
		assert.Contains(t, string(data), "allowedSignersFile = ~/.ssh/allowed_signers")
	})

	t.Run("marshal gpg format and ssh subsection", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.SetGPGFormat("openpgp")
		cfg.SetGPGSSHAllowedSignersFile("/etc/ssh/allowed_signers")

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), "[gpg]")
		assert.Contains(t, string(data), "format = openpgp")
		assert.Contains(t, string(data), `[gpg "ssh"]`)
		assert.Contains(t, string(data), "allowedSignersFile = /etc/ssh/allowed_signers")
	})

	t.Run("round-trip marshal/unmarshal", func(t *testing.T) {
		t.Parallel()
		original := NewConfig()
		original.SetGPGFormat("ssh")
		original.SetGPGSSHAllowedSignersFile("~/.ssh/allowed_signers")

		data, err := original.Marshal()
		require.NoError(t, err)

		parsed := NewConfig()
		err = parsed.Unmarshal(data)
		require.NoError(t, err)

		assert.Equal(t, "ssh", parsed.GPG().Format)
		assert.Equal(t, "~/.ssh/allowed_signers", parsed.GPG().SSH.AllowedSignersFile)
	})

	t.Run("empty gpg config not marshaled", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.SetUser(User{Name: "Test User"})

		data, err := cfg.Marshal()
		require.NoError(t, err)

		// Empty GPG config should not be included
		configStr := string(data)
		if strings.Contains(configStr, "[gpg]") {
			// Check that it's not an empty section
			lines := strings.Split(configStr, "\n")
			for i, line := range lines {
				if strings.TrimSpace(line) == "[gpg]" {
					// Check if next non-empty line is another section or EOF
					for j := i + 1; j < len(lines); j++ {
						nextLine := strings.TrimSpace(lines[j])
						if nextLine == "" {
							continue
						}
						// If next line is a section header or there's no format key, fail
						if strings.HasPrefix(nextLine, "[") || !strings.Contains(nextLine, "format") {
							t.Error("Empty [gpg] section should not be marshaled")
						}
						break
					}
				}
			}
		}
	})

	t.Run("unmarshal with openpgp format", func(t *testing.T) {
		t.Parallel()
		input := []byte(`[gpg]
	format = openpgp
`)
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, "openpgp", cfg.GPG().Format)
	})

	t.Run("unmarshal user.signingKey", func(t *testing.T) {
		t.Parallel()
		input := []byte("[user]\n\tsigningKey = ~/.ssh/rsa_id")
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, "~/.ssh/rsa_id", cfg.User().SigningKey)
	})

	t.Run("marshal user.signingKey", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.SetUser(User{SigningKey: "/path/to/key"})

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), "[user]\n\tsigningKey = /path/to/key")
	})

	t.Run("unmarshal gpgSign true", func(t *testing.T) {
		t.Parallel()
		input := []byte("[commit]\n\tgpgSign = true\n[tag]\n\tgpgSign = true")
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, OptBoolTrue, cfg.Tag().GpgSign)
		assert.Equal(t, OptBoolTrue, cfg.Commit().GpgSign)
	})

	t.Run("unmarshal gpgSign false", func(t *testing.T) {
		t.Parallel()
		input := []byte("[commit]\n\tgpgSign = false\n[tag]\n\tgpgSign = false")
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, OptBoolFalse, cfg.Tag().GpgSign)
		assert.Equal(t, OptBoolFalse, cfg.Commit().GpgSign)
	})

	t.Run("unmarshal gpgSign unset", func(t *testing.T) {
		t.Parallel()
		input := []byte("[core]\n\tbare = false")
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, OptBoolUnset, cfg.Tag().GpgSign)
		assert.Equal(t, OptBoolUnset, cfg.Commit().GpgSign)
	})

	t.Run("marshal gpgSign", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.SetTagGpgSign(OptBoolTrue)
		cfg.SetCommitGpgSign(OptBoolTrue)

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), "[commit]\n\tgpgSign = true")
		assert.Contains(t, string(data), "[tag]\n\tgpgSign = true")
	})

	t.Run("marshal gpgSign false", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.SetTagGpgSign(OptBoolFalse)
		cfg.SetCommitGpgSign(OptBoolFalse)

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), "[commit]\n\tgpgSign = false")
		assert.Contains(t, string(data), "[tag]\n\tgpgSign = false")
	})

	t.Run("merge gpgSign false overrides true", func(t *testing.T) {
		t.Parallel()
		global := NewConfig()
		global.SetTagGpgSign(OptBoolTrue)
		global.SetCommitGpgSign(OptBoolTrue)

		local := NewConfig()
		local.SetTagGpgSign(OptBoolFalse)
		local.SetCommitGpgSign(OptBoolFalse)

		merged := Merge(global, local)

		assert.Equal(t, OptBoolFalse, merged.Tag().GpgSign)
		assert.Equal(t, OptBoolFalse, merged.Commit().GpgSign)
	})

	t.Run("merge keeps gpgSign false if next config unset", func(t *testing.T) {
		t.Parallel()
		global := NewConfig()
		global.SetCommitGpgSign(OptBoolFalse)

		local := NewConfig()
		local.SetTagGpgSign(OptBoolFalse)

		merged := Merge(global, local)

		assert.True(t, merged.Tag().GpgSign.IsSet())
		assert.True(t, merged.Commit().GpgSign.IsSet())
		assert.Equal(t, OptBoolFalse, merged.Tag().GpgSign)
		assert.Equal(t, OptBoolFalse, merged.Commit().GpgSign)
	})
}
