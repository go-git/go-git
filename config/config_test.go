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

	s.True(cfg.Core.IsBare)
	s.Equal("foo", cfg.Core.Worktree)
	s.Equal("bar", cfg.Core.CommentChar)
	s.Equal("true", cfg.Core.AutoCRLF)
	s.False(cfg.Core.FileMode)
	s.Equal("custom-hooks", cfg.Core.HooksPath)
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
	filemode = true
	hooksPath = custom-hooks
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
	cfg.Core.HooksPath = "custom-hooks"
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
				c.Core.RepositoryFormatVersion = config.Version1
				c.Extensions.WorktreeConfig = true
			},
			wantSection: true,
		},
		{
			name: "ObjectFormat set writes section",
			setup: func(c *Config) {
				c.Core.RepositoryFormatVersion = config.Version1
				c.Extensions.ObjectFormat = config.SHA256
			},
			wantSection: true,
		},
		{
			name: "RepositoryFormat = 0 ignores section",
			setup: func(c *Config) {
				c.Core.RepositoryFormatVersion = config.Version0
				c.Extensions.ObjectFormat = config.SHA256
				c.Extensions.WorktreeConfig = true
			},
			wantSection: false,
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
	cfg.User.Name = "foo"
	cfg.User.Email = "foo@foo.com"

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

	s.Equal("foo@foo.com", cfg.User.Email)
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

func (s *ConfigSuite) TestValidateBranchNonRefsPrefix() {
	// Real git allows any value for branch.*.merge, including values
	// without a refs/ prefix. Validate should accept them.
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

	s.NoError(config.Validate())
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

			assert.Equal(t, tc.wantRead, cfg.Pack.ReadReverseIndex)
			assert.Equal(t, tc.wantWrite, cfg.Pack.WriteReverseIndex)
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
			cfg.Pack.ReadReverseIndex = tc.read
			cfg.Pack.WriteReverseIndex = tc.write

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

			assert.Equal(t, tc.want, cfg.Index.SkipHash)
		})
	}
}

func TestMarshalIndexSkipHash(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	cfg.Index.SkipHash = OptBoolTrue

	b, err := cfg.Marshal()
	require.NoError(t, err)
	assert.Contains(t, string(b), "skipHash = true")

	// Round-trip: unmarshal the marshaled output and verify.
	cfg2 := NewConfig()
	err = cfg2.Unmarshal(b)
	require.NoError(t, err)
	assert.Equal(t, OptBoolTrue, cfg2.Index.SkipHash)
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
				{User: user{
					Name: "foo", Email: "bar@test",
				}},
				{
					Extensions: struct {
						ObjectFormat   config.ObjectFormat
						WorktreeConfig bool
					}{
						ObjectFormat:   config.SHA256,
						WorktreeConfig: true,
					},
				},
			},
			want: Config{
				User: user{
					Name:  "foo",
					Email: "bar@test",
				},
				Extensions: struct {
					ObjectFormat   config.ObjectFormat
					WorktreeConfig bool
				}{
					ObjectFormat:   config.SHA256,
					WorktreeConfig: true,
				},
			},
		},
		{
			name: "merge nested fields",
			input: []*Config{
				{User: user{Name: "foo"}},
				{User: user{Email: "bar@test"}},
			},
			want: Config{
				User: user{
					Name:  "foo",
					Email: "bar@test",
				},
			},
		},
		{
			name: "override nested fields",
			input: []*Config{
				{User: user{Name: "foo"}},
				{User: user{Name: "bar", Email: "foo@test"}},
			},
			want: Config{
				User: user{
					Name:  "bar",
					Email: "foo@test",
				},
			},
		},
		{
			name: "src nil map preserves dst map",
			input: []*Config{
				{
					Remotes: map[string]*RemoteConfig{
						"origin": {Name: "origin", URLs: []string{"https://example.com/repo.git"}},
					},
				},
				{
					// Remotes is nil (zero value)
					Branches: map[string]*Branch{
						"main": {Name: "main"},
					},
				},
			},
			want: Config{
				Remotes: map[string]*RemoteConfig{
					"origin": {Name: "origin", URLs: []string{"https://example.com/repo.git"}},
				},
				Branches: map[string]*Branch{
					"main": {Name: "main"},
				},
			},
		},
		{
			name: "src empty map preserves dst map",
			input: []*Config{
				{
					Remotes: map[string]*RemoteConfig{
						"origin": {Name: "origin", URLs: []string{"https://example.com/repo.git"}},
					},
				},
				{
					// Remotes is explicitly initialised but empty (mirrors NewConfig behaviour).
					Remotes: map[string]*RemoteConfig{},
				},
			},
			want: Config{
				Remotes: map[string]*RemoteConfig{
					"origin": {Name: "origin", URLs: []string{"https://example.com/repo.git"}},
				},
			},
		},
		{
			name: "merge maps with disjoint keys",
			input: []*Config{
				{
					Remotes: map[string]*RemoteConfig{
						"origin": {Name: "origin", URLs: []string{"https://example.com/repo.git"}},
					},
				},
				{
					Remotes: map[string]*RemoteConfig{
						"upstream": {Name: "upstream", URLs: []string{"https://upstream.com/repo.git"}},
					},
				},
			},
			want: Config{
				Remotes: map[string]*RemoteConfig{
					"origin":   {Name: "origin", URLs: []string{"https://example.com/repo.git"}},
					"upstream": {Name: "upstream", URLs: []string{"https://upstream.com/repo.git"}},
				},
			},
		},
		{
			name: "src map entry overrides dst map entry",
			input: []*Config{
				{
					Remotes: map[string]*RemoteConfig{
						"origin": {Name: "origin", URLs: []string{"https://old.com/repo.git"}},
					},
				},
				{
					Remotes: map[string]*RemoteConfig{
						"origin": {Name: "origin", URLs: []string{"https://new.com/repo.git"}},
					},
				},
			},
			want: Config{
				Remotes: map[string]*RemoteConfig{
					"origin": {Name: "origin", URLs: []string{"https://new.com/repo.git"}},
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

		assert.Equal(t, "wt-user", merged.User.Name)
		assert.Equal(t, "base@example.com", merged.User.Email)
		assert.True(t, merged.Extensions.WorktreeConfig)

		require.Nil(t, merged.Raw, "merged Raw must be nil")

		_, err = merged.Marshal()
		require.NoError(t, err)

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

		assert.Equal(t, "ssh", cfg.GPG.Format)
	})

	t.Run("unmarshal gpg.ssh.allowedSignersFile", func(t *testing.T) {
		t.Parallel()
		input := []byte(`[gpg "ssh"]
	allowedSignersFile = ~/.ssh/allowed_signers
`)
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, "~/.ssh/allowed_signers", cfg.GPG.SSH.AllowedSignersFile)
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

		assert.Equal(t, "ssh", cfg.GPG.Format)
		assert.Equal(t, "/home/user/.ssh/allowed_signers", cfg.GPG.SSH.AllowedSignersFile)
	})

	t.Run("marshal gpg format", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.GPG.Format = "ssh"

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), "[gpg]")
		assert.Contains(t, string(data), "format = ssh")
	})

	t.Run("marshal gpg.ssh.allowedSignersFile", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.GPG.SSH.AllowedSignersFile = "~/.ssh/allowed_signers"

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), `[gpg "ssh"]`)
		assert.Contains(t, string(data), "allowedSignersFile = ~/.ssh/allowed_signers")
	})

	t.Run("marshal gpg format and ssh subsection", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.GPG.Format = "openpgp"
		cfg.GPG.SSH.AllowedSignersFile = "/etc/ssh/allowed_signers"

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
		original.GPG.Format = "ssh"
		original.GPG.SSH.AllowedSignersFile = "~/.ssh/allowed_signers"

		data, err := original.Marshal()
		require.NoError(t, err)

		parsed := NewConfig()
		err = parsed.Unmarshal(data)
		require.NoError(t, err)

		assert.Equal(t, "ssh", parsed.GPG.Format)
		assert.Equal(t, "~/.ssh/allowed_signers", parsed.GPG.SSH.AllowedSignersFile)
	})

	t.Run("empty gpg config not marshaled", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.User.Name = "Test User"

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

		assert.Equal(t, "openpgp", cfg.GPG.Format)
	})

	t.Run("unmarshal user.signingKey", func(t *testing.T) {
		t.Parallel()
		input := []byte("[user]\n\tsigningKey = ~/.ssh/rsa_id")
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, "~/.ssh/rsa_id", cfg.User.SigningKey)
	})

	t.Run("marshal user.signingKey", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.User.SigningKey = "/path/to/key"

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

		assert.Equal(t, OptBoolTrue, cfg.Tag.GpgSign)
		assert.Equal(t, OptBoolTrue, cfg.Commit.GpgSign)
	})

	t.Run("unmarshal gpgSign false", func(t *testing.T) {
		t.Parallel()
		input := []byte("[commit]\n\tgpgSign = false\n[tag]\n\tgpgSign = false")
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, OptBoolFalse, cfg.Tag.GpgSign)
		assert.Equal(t, OptBoolFalse, cfg.Commit.GpgSign)
	})

	t.Run("unmarshal gpgSign unset", func(t *testing.T) {
		t.Parallel()
		input := []byte("[core]\n\tbare = false")
		cfg := NewConfig()
		err := cfg.Unmarshal(input)
		require.NoError(t, err)

		assert.Equal(t, OptBoolUnset, cfg.Tag.GpgSign)
		assert.Equal(t, OptBoolUnset, cfg.Commit.GpgSign)
	})

	t.Run("marshal gpgSign", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.Tag.GpgSign = OptBoolTrue
		cfg.Commit.GpgSign = OptBoolTrue

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), "[commit]\n\tgpgSign = true")
		assert.Contains(t, string(data), "[tag]\n\tgpgSign = true")
	})

	t.Run("marshal gpgSign false", func(t *testing.T) {
		t.Parallel()
		cfg := NewConfig()
		cfg.Tag.GpgSign = OptBoolFalse
		cfg.Commit.GpgSign = OptBoolFalse

		data, err := cfg.Marshal()
		require.NoError(t, err)

		assert.Contains(t, string(data), "[commit]\n\tgpgSign = false")
		assert.Contains(t, string(data), "[tag]\n\tgpgSign = false")
	})

	t.Run("merge gpgSign false overrides true", func(t *testing.T) {
		t.Parallel()
		global := NewConfig()
		global.Tag.GpgSign = OptBoolTrue
		global.Commit.GpgSign = OptBoolTrue

		local := NewConfig()
		local.Tag.GpgSign = OptBoolFalse
		local.Commit.GpgSign = OptBoolFalse

		merged := Merge(global, local)

		assert.Equal(t, OptBoolFalse, merged.Tag.GpgSign)
		assert.Equal(t, OptBoolFalse, merged.Commit.GpgSign)
	})

	t.Run("merge keeps gpgSign false if next config unset", func(t *testing.T) {
		t.Parallel()
		global := NewConfig()
		global.Commit.GpgSign = OptBoolFalse

		local := NewConfig()
		local.Tag.GpgSign = OptBoolFalse

		merged := Merge(global, local)

		assert.True(t, merged.Tag.GpgSign.IsSet())
		assert.True(t, merged.Commit.GpgSign.IsSet())
		assert.Equal(t, OptBoolFalse, merged.Tag.GpgSign)
		assert.Equal(t, OptBoolFalse, merged.Commit.GpgSign)
	})
}
