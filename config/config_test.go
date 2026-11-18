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

	cfg.URLs = append(cfg.URLs, &URL{
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

	// A non-default version is written back out.
	cfg.Protocol.Version = protocol.V0
	buf, err = cfg.Marshal()
	s.NoError(err)
	if !strings.Contains(string(buf), "version = 0") {
		s.Fail("marshal did not update version")
	}

	// The default version is omitted from the marshalled config.
	cfg = NewConfig()
	cfg.Protocol.Version = DefaultProtocolVersion
	buf, err = cfg.Marshal()
	s.NoError(err)
	if strings.Contains(string(buf), "[protocol]") {
		s.Fail("marshal should omit the default protocol version")
	}

	// Resetting a non-default version back to the default clears the stale key
	// from the raw config so the change persists.
	cfg = NewConfig()
	err = cfg.Unmarshal([]byte("[protocol]\n\tversion = 1"))
	s.NoError(err)
	s.Equal(protocol.V1, cfg.Protocol.Version)
	cfg.Protocol.Version = DefaultProtocolVersion
	buf, err = cfg.Marshal()
	s.NoError(err)
	if strings.Contains(string(buf), "version = 1") {
		s.Fail("marshal should drop the stale protocol version")
	}

	// The reset round-trips back to the default version.
	cfg = NewConfig()
	err = cfg.Unmarshal(buf)
	s.NoError(err)
	s.Equal(DefaultProtocolVersion, cfg.Protocol.Version)
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

// TestUnmarshalRemotePartialClone reads back the partial-clone keys exactly as
// the git binary writes them for a `clone --filter=blob:none`.
func TestUnmarshalRemotePartialClone(t *testing.T) {
	t.Parallel()

	input := []byte(`[core]
	repositoryformatversion = 1
[remote "origin"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	promisor = true
	partialclonefilter = blob:none
`)

	cfg := NewConfig()
	require.NoError(t, cfg.Unmarshal(input))

	origin := cfg.Remotes["origin"]
	require.NotNil(t, origin)
	assert.True(t, origin.Promisor)
	assert.Equal(t, "blob:none", origin.PartialCloneFilter)
}

func TestMarshalRemotePartialClone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		promisor bool
		filter   string
		want     []string
		notWant  []string
	}{
		{
			name:     "promisor with filter",
			promisor: true,
			filter:   "blob:none",
			want:     []string{"promisor = true", "partialclonefilter = blob:none"},
		},
		{
			name:     "tree filter",
			promisor: true,
			filter:   "tree:0",
			want:     []string{"promisor = true", "partialclonefilter = tree:0"},
		},
		{
			name:    "neither set writes no partial-clone keys",
			notWant: []string{"promisor", "partialclonefilter"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewConfig()
			cfg.Remotes["origin"] = &RemoteConfig{
				Name:               "origin",
				URLs:               []string{"https://github.com/go-git/go-git.git"},
				Promisor:           tc.promisor,
				PartialCloneFilter: tc.filter,
			}

			b, err := cfg.Marshal()
			require.NoError(t, err)

			for _, want := range tc.want {
				assert.Contains(t, string(b), want)
			}
			for _, notWant := range tc.notWant {
				assert.NotContains(t, string(b), notWant)
			}
		})
	}
}

// TestMarshalRemoteClearsPartialClone covers the state behind the fetch
// failures this config exists to prevent: a promisor = true left behind without
// a partialclonefilter makes git fetch unfiltered into a repository that is
// missing objects, which then fails resolving deltas against absent bases.
// Clearing either field must remove the key rather than leave it on disk.
func TestMarshalRemoteClearsPartialClone(t *testing.T) {
	t.Parallel()

	input := []byte(`[remote "origin"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	promisor = true
	partialclonefilter = blob:none
`)

	cfg := NewConfig()
	require.NoError(t, cfg.Unmarshal(input))

	cfg.Remotes["origin"].Promisor = false
	cfg.Remotes["origin"].PartialCloneFilter = ""

	b, err := cfg.Marshal()
	require.NoError(t, err)

	assert.NotContains(t, string(b), "promisor")
	assert.NotContains(t, string(b), "partialclonefilter")
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

func TestUnmarshalRemoteEmptyURLResets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty url clears earlier urls",
			input: "[remote \"origin\"]\n\turl = https://example.com/a.git\n\turl = \"\"\n",
			want:  nil,
		},
		{
			name: "urls after an empty url are kept",
			input: "[remote \"origin\"]\n\turl = https://example.com/a.git\n\turl = \"\"\n" +
				"\turl = https://example.com/b.git\n",
			want: []string{"https://example.com/b.git"},
		},
		{
			name: "empty pushurl clears only earlier pushurls",
			input: "[remote \"origin\"]\n\turl = https://example.com/a.git\n" +
				"\tpushurl = https://example.com/p.git\n\tpushurl = \"\"\n",
			want: []string{"https://example.com/a.git"},
		},
		{
			name: "empty url keeps pushurls",
			input: "[remote \"origin\"]\n\turl = https://example.com/a.git\n" +
				"\tpushurl = https://example.com/p.git\n\turl = \"\"\n",
			want: []string{"https://example.com/p.git"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewConfig()
			require.NoError(t, cfg.Unmarshal([]byte(tc.input)))

			require.Contains(t, cfg.Remotes, "origin")
			assert.Equal(t, tc.want, cfg.Remotes["origin"].URLs)

			b, err := cfg.Marshal()
			require.NoError(t, err)

			again := NewConfig()
			require.NoError(t, again.Unmarshal(b))
			require.Contains(t, again.Remotes, "origin")
			assert.Equal(t, tc.want, again.Remotes["origin"].URLs)
		})
	}
}

func TestMarshalRemoteKeepsEmptyURL(t *testing.T) {
	t.Parallel()

	const input = "[remote \"origin\"]\n\turl = \"\"\n"

	cfg := NewConfig()
	require.NoError(t, cfg.Unmarshal([]byte(input)))

	b, err := cfg.Marshal()
	require.NoError(t, err)

	got := Merge(
		mustReadConfig(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"),
		mustReadConfig(t, string(b)),
	)
	require.Contains(t, got.Remotes, "origin")
	assert.Empty(t, got.Remotes["origin"].URLs)
}

func TestMergeRawAddsNoEmptySections(t *testing.T) {
	t.Parallel()

	typed := NewConfig()
	typed.User.Name = "x"

	raw := typed.mergeRaw()
	require.NotNil(t, raw)
	assert.Equal(t, []string{"x"}, raw.Section("user").OptionAll("name"))
	for _, s := range raw.Sections {
		assert.Truef(t, len(s.Options) > 0 || len(s.Subsections) > 0, "section %q is empty", s.Name)
	}
}

func mustReadConfig(t *testing.T, s string) *Config {
	t.Helper()

	cfg, err := ReadConfig(strings.NewReader(s))
	require.NoError(t, err)

	return cfg
}

func TestMerge(t *testing.T) {
	t.Parallel()

	read := func(t *testing.T, s string) *Config {
		t.Helper()
		cfg, err := ReadConfig(strings.NewReader(s))
		require.NoError(t, err)
		return cfg
	}

	tests := []struct {
		name   string
		input  func(t *testing.T) []*Config
		assert func(t *testing.T, got Config)
	}{
		{
			name: "later false overrides earlier true",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[core]\n\tfilemode = true\n[pack]\n\treadReverseIndex = true\n"),
					read(t, "[core]\n\tfilemode = false\n[pack]\n\treadReverseIndex = false\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				assert.False(t, got.Core.FileMode)
				assert.False(t, got.Pack.ReadReverseIndex)
			},
		},
		{
			name: "later source without key keeps earlier value",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[core]\n\tfilemode = false\n[pack]\n\twindow = 5\n"),
					read(t, "[user]\n\tname = foo\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				assert.False(t, got.Core.FileMode)
				assert.Equal(t, uint(5), got.Pack.Window)
				assert.Equal(t, "foo", got.User.Name)
			},
		},
		{
			name: "insteadOf rules from all sources are kept",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[url \"https://github.com/\"]\n\tinsteadOf = gh:\n"),
					read(t, "[url \"https://gitlab.com/\"]\n\tinsteadOf = gl:\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				require.Len(t, got.URLs, 2)
				assert.Equal(t, "https://github.com/", got.URLs[0].Name)
				assert.Equal(t, []string{"gh:"}, got.URLs[0].InsteadOfs)
				assert.Equal(t, "https://gitlab.com/", got.URLs[1].Name)
				assert.Equal(t, []string{"gl:"}, got.URLs[1].InsteadOfs)
			},
		},
		{
			name: "insteadOf values for the same base accumulate",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[url \"https://github.com/\"]\n\tinsteadOf = gh:\n"),
					read(t, "[url \"https://github.com/\"]\n\tinsteadOf = github:\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				require.Len(t, got.URLs, 1)
				assert.Equal(t, []string{"gh:", "github:"}, got.URLs[0].InsteadOfs)
				assert.Equal(t, "https://github.com/x/y", got.URLs[0].ApplyInsteadOf("gh:x/y"))
				assert.Equal(t, "https://github.com/x/y", got.URLs[0].ApplyInsteadOf("github:x/y"))
			},
		},
		{
			name: "insteadOf rules apply to remotes from other sources",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[url \"https://github.com/\"]\n\tinsteadOf = gh:\n"),
					read(t, "[remote \"origin\"]\n\turl = gh:x/y\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				require.Contains(t, got.Remotes, "origin")
				assert.Equal(t, []string{"https://github.com/x/y"}, got.Remotes["origin"].URLs)
			},
		},
		{
			name: "repeated subsections accumulate multi-valued keys",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"+
						"\tfetch = +refs/heads/*:refs/remotes/origin/*\n"),
					read(t, "[remote \"origin\"]\n\tfetch = +refs/tags/*:refs/tags/*\n"+
						"[remote \"upstream\"]\n\turl = https://example.com/b.git\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				require.Contains(t, got.Remotes, "origin")
				assert.Equal(t, []string{"https://example.com/a.git"}, got.Remotes["origin"].URLs)
				assert.Equal(t, []RefSpec{
					"+refs/heads/*:refs/remotes/origin/*",
					"+refs/tags/*:refs/tags/*",
				}, got.Remotes["origin"].Fetch)
				require.Contains(t, got.Remotes, "upstream")
				assert.Equal(t, []string{"https://example.com/b.git"}, got.Remotes["upstream"].URLs)
			},
		},
		{
			name: "empty url in a later source clears earlier urls",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"+
						"\tfetch = +refs/heads/*:refs/remotes/origin/*\n"),
					read(t, "[remote \"origin\"]\n\turl = \"\"\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				require.Contains(t, got.Remotes, "origin")
				assert.Empty(t, got.Remotes["origin"].URLs)
				assert.Equal(t, []RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, got.Remotes["origin"].Fetch)
			},
		},
		{
			name: "url after an empty url in a later source replaces earlier urls",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"),
					read(t, "[remote \"origin\"]\n\turl = \"\"\n\turl = https://example.com/b.git\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				require.Contains(t, got.Remotes, "origin")
				assert.Equal(t, []string{"https://example.com/b.git"}, got.Remotes["origin"].URLs)
			},
		},
		{
			name: "empty pushurl in a later source clears earlier pushurls",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"+
						"\tpushurl = https://example.com/p.git\n"),
					read(t, "[remote \"origin\"]\n\tpushurl = \"\"\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				require.Contains(t, got.Remotes, "origin")
				assert.Equal(t, []string{"https://example.com/a.git"}, got.Remotes["origin"].URLs)
			},
		},
		{
			name: "typed source adds a branch without options",
			input: func(t *testing.T) []*Config {
				typed := NewConfig()
				typed.Branches["topic"] = &Branch{Name: "topic"}
				return []*Config{read(t, "[user]\n\tname = a\n"), typed}
			},
			assert: func(t *testing.T, got Config) {
				assert.Contains(t, got.Branches, "topic")
			},
		},
		{
			name: "typed-only source",
			input: func(t *testing.T) []*Config {
				typed := NewConfig()
				typed.Raw = nil
				typed.User.Name = "typed"
				typed.Pack.Window = 3
				return []*Config{
					read(t, "[user]\n\tname = file\n\temail = file@example.com\n"),
					typed,
				}
			},
			assert: func(t *testing.T, got Config) {
				assert.Equal(t, "typed", got.User.Name)
				assert.Equal(t, "file@example.com", got.User.Email)
				assert.Equal(t, uint(3), got.Pack.Window)
			},
		},
		{
			name: "raw-only source",
			input: func(t *testing.T) []*Config {
				rawOnly := NewConfig()
				rawOnly.Raw.SetOption("user", "", "name", "raw")
				rawOnly.Raw.SetOption("custom", "", "key", "value")
				return []*Config{
					read(t, "[user]\n\tname = file\n"),
					rawOnly,
				}
			},
			assert: func(t *testing.T, got Config) {
				assert.Equal(t, "raw", got.User.Name)
				require.NotNil(t, got.Raw)
				assert.Equal(t, "value", got.Raw.Section("custom").Option("key"))
			},
		},
		{
			name: "stale Raw uses typed fields",
			input: func(t *testing.T) []*Config {
				stale := read(t, "[core]\n\tfilemode = true\n[user]\n\tname = old\n")
				stale.Core.FileMode = false
				stale.User.Name = "new"
				return []*Config{stale}
			},
			assert: func(t *testing.T, got Config) {
				assert.False(t, got.Core.FileMode)
				assert.Equal(t, "new", got.User.Name)
			},
		},
		{
			name: "stale source does not override keys it never set",
			input: func(t *testing.T) []*Config {
				global := read(t, "[user]\n\tname = a\n")
				global.User.Email = "a@example.com"
				return []*Config{read(t, "[core]\n\tfilemode = false\n"), global}
			},
			assert: func(t *testing.T, got Config) {
				assert.False(t, got.Core.FileMode)
				assert.Equal(t, "a@example.com", got.User.Email)
				require.NotNil(t, got.Raw)
				assert.False(t, got.Raw.Section("core").HasOption("bare"))
			},
		},
		{
			name: "changed NewConfig does not override keys it never set",
			input: func(t *testing.T) []*Config {
				typed := NewConfig()
				typed.User.Name = "x"
				return []*Config{read(t, "[core]\n\tfilemode = false\n[pack]\n\twindow = 5\n"), typed}
			},
			assert: func(t *testing.T, got Config) {
				assert.Equal(t, "x", got.User.Name)
				assert.False(t, got.Core.FileMode)
				assert.Equal(t, uint(5), got.Pack.Window)
			},
		},
		{
			name: "stale source keeps pushurl apart from url",
			input: func(t *testing.T) []*Config {
				stale := read(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"+
					"\tpushurl = https://example.com/p.git\n")
				stale.User.Name = "x"
				return []*Config{stale}
			},
			assert: func(t *testing.T, got Config) {
				require.Contains(t, got.Remotes, "origin")
				assert.Equal(t, []string{
					"https://example.com/a.git",
					"https://example.com/p.git",
				}, got.Remotes["origin"].URLs)
				origin := got.Raw.Section("remote").Subsection("origin")
				assert.Equal(t, []string{"https://example.com/a.git"}, origin.OptionAll("url"))
				assert.Equal(t, []string{"https://example.com/p.git"}, origin.OptionAll("pushurl"))
			},
		},
		{
			name: "stale source removes a deleted remote",
			input: func(t *testing.T) []*Config {
				stale := read(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"+
					"[remote \"upstream\"]\n\turl = https://example.com/b.git\n")
				delete(stale.Remotes, "upstream")
				return []*Config{stale}
			},
			assert: func(t *testing.T, got Config) {
				assert.Contains(t, got.Remotes, "origin")
				assert.NotContains(t, got.Remotes, "upstream")
			},
		},
		{
			name: "zero values of a Config literal count as set",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[pack]\n\twindow = 5\n"),
					{User: user{Name: "x"}},
				}
			},
			assert: func(t *testing.T, got Config) {
				assert.Equal(t, "x", got.User.Name)
				assert.False(t, got.Core.FileMode)
				assert.Equal(t, uint(0), got.Pack.Window)
				assert.False(t, got.Pack.ReadReverseIndex)
				assert.Equal(t, protocol.V0, got.Protocol.Version)
			},
		},
		{
			name: "invalid typed change falls back to the source Raw",
			input: func(t *testing.T) []*Config {
				invalid := read(t, "[branch \"main\"]\n\tremote = origin\n")
				invalid.Branches["main"].Rebase = "bogus"
				invalid.User.Name = "dropped"
				return []*Config{
					read(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"+
						"[url \"https://github.com/\"]\n\tinsteadOf = gh:\n"+
						"[protocol]\n\tversion = 1\n"),
					invalid,
				}
			},
			assert: func(t *testing.T, got Config) {
				require.Contains(t, got.Branches, "main")
				assert.Equal(t, "origin", got.Branches["main"].Remote)
				assert.Empty(t, got.Branches["main"].Rebase)
				assert.Empty(t, got.User.Name)
				assert.Contains(t, got.Remotes, "origin")
				assert.Len(t, got.URLs, 1)
				assert.Equal(t, protocol.V1, got.Protocol.Version)
			},
		},
		{
			name: "source with an unreadable Raw is skipped",
			input: func(t *testing.T) []*Config {
				invalid := NewConfig()
				invalid.Raw.SetOption("pack", "", "window", "bogus")
				invalid.Raw.SetOption("user", "", "name", "dropped")
				return []*Config{
					read(t, "[remote \"origin\"]\n\turl = https://example.com/a.git\n"),
					invalid,
				}
			},
			assert: func(t *testing.T, got Config) {
				assert.Contains(t, got.Remotes, "origin")
				assert.Equal(t, DefaultPackWindow, got.Pack.Window)
				assert.Empty(t, got.User.Name)
			},
		},
		{
			name: "section names match case-insensitively and subsection names do not",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[Core]\n\tFileMode = false\n"+
						"[remote \"Origin\"]\n\turl = https://example.com/a.git\n"),
					read(t, "[core]\n\tbare = true\n"+
						"[remote \"origin\"]\n\turl = https://example.com/b.git\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				assert.False(t, got.Core.FileMode)
				assert.True(t, got.Core.IsBare)
				require.Contains(t, got.Remotes, "Origin")
				require.Contains(t, got.Remotes, "origin")
				assert.Equal(t, []string{"https://example.com/a.git"}, got.Remotes["Origin"].URLs)
				assert.Equal(t, []string{"https://example.com/b.git"}, got.Remotes["origin"].URLs)
			},
		},
		{
			name: "placeholder sources add no keys",
			input: func(t *testing.T) []*Config {
				return []*Config{
					NewConfig(),
					nil,
					read(t, "[core]\n\tfilemode = false\n"),
					NewConfig(),
				}
			},
			assert: func(t *testing.T, got Config) {
				assert.False(t, got.Core.FileMode)
				require.NotNil(t, got.Raw)
				assert.False(t, got.Raw.Section("core").HasOption("bare"))
				assert.Equal(t, []string{"false"}, got.Raw.Section("core").OptionAll("filemode"))
			},
		},
		{
			name: "merged Raw holds keys from all sources",
			input: func(t *testing.T) []*Config {
				return []*Config{
					read(t, "[core]\n\trepositoryformatversion = 1\n"+
						"[extensions]\n\tworktreeConfig = true\n"+
						"[user]\n\tname = base-user\n\temail = base@example.com\n"),
					read(t, "[user]\n\tname = wt-user\n[custom]\n\tkey = value\n"),
				}
			},
			assert: func(t *testing.T, got Config) {
				require.NotNil(t, got.Raw)
				assert.True(t, got.Extensions.WorktreeConfig)
				assert.Equal(t, "wt-user", got.User.Name)
				assert.Equal(t, "base@example.com", got.User.Email)
				assert.Equal(t, "true", got.Raw.Section("extensions").Option("worktreeConfig"))
				assert.Equal(t, []string{"base-user", "wt-user"}, got.Raw.Section("user").OptionAll("name"))
				assert.Equal(t, "value", got.Raw.Section("custom").Option("key"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, Merge(tc.input(t)...))
		})
	}

	t.Run("no sources", func(t *testing.T) {
		t.Parallel()

		got := Merge()

		assert.Equal(t, *read(t, ""), got)
	})

	t.Run("sources are not modified", func(t *testing.T) {
		t.Parallel()

		const input = "[core]\n\tfilemode = true\n" +
			"[remote \"origin\"]\n\turl = https://example.com/a.git\n" +
			"[branch \"main\"]\n\tremote = origin\n" +
			"[url \"https://github.com/\"]\n\tinsteadOf = gh:\n"
		stale := read(t, input)
		stale.Core.FileMode = false
		stale.Remotes["origin"].URLs = []string{"https://example.com/b.git"}
		stale.Branches["main"].Remote = "upstream"
		stale.URLs[0].InsteadOfs = []string{"github:"}

		var before strings.Builder
		require.NoError(t, config.NewEncoder(&before).Encode(stale.Raw))

		got := Merge(stale)

		var after strings.Builder
		require.NoError(t, config.NewEncoder(&after).Encode(stale.Raw))
		assert.Equal(t, before.String(), after.String())
		assert.False(t, got.Core.FileMode)
		assert.Equal(t, []string{"https://example.com/b.git"}, got.Remotes["origin"].URLs)
		assert.Equal(t, "upstream", got.Branches["main"].Remote)
		assert.Equal(t, []string{"github:"}, got.URLs[0].InsteadOfs)
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

// Git accepts yes/no/on/off and any integer for a boolean, not just the set
// strconv.ParseBool understands. Expectations are the output of
// `git config -f <file> --type=bool --get <key>` under git 2.50.1.
func (s *ConfigSuite) TestUnmarshalBoolValues() {
	for _, tc := range []struct {
		value string
		want  OptBool
	}{
		{value: "true", want: OptBoolTrue},
		{value: "TRUE", want: OptBoolTrue},
		{value: "yes", want: OptBoolTrue},
		{value: "Yes", want: OptBoolTrue},
		{value: "on", want: OptBoolTrue},
		{value: "On", want: OptBoolTrue},
		{value: "1", want: OptBoolTrue},
		{value: "2", want: OptBoolTrue},
		{value: "-1", want: OptBoolTrue},
		{value: "false", want: OptBoolFalse},
		{value: "no", want: OptBoolFalse},
		{value: "off", want: OptBoolFalse},
		{value: "0", want: OptBoolFalse},
		// git errors on a value it cannot read; go-git leaves the key unset so
		// the caller's default applies.
		{value: "garbage", want: OptBoolUnset},
	} {
		input := []byte("[commit]\n\tgpgsign = " + tc.value +
			"\n[tag]\n\tgpgsign = " + tc.value +
			"\n[index]\n\tskipHash = " + tc.value +
			"\n[uploadarchive]\n\tallowUnreachable = " + tc.value +
			"\n[extensions]\n\tworktreeConfig = " + tc.value + "\n")

		cfg := NewConfig()
		s.Require().NoError(cfg.Unmarshal(input), tc.value)

		s.Equal(tc.want, cfg.Commit.GpgSign, "commit.gpgsign = %s", tc.value)
		s.Equal(tc.want, cfg.Tag.GpgSign, "tag.gpgsign = %s", tc.value)
		s.Equal(tc.want, cfg.Index.SkipHash, "index.skipHash = %s", tc.value)
		s.Equal(tc.want, cfg.UploadArchive.AllowUnreachable,
			"uploadarchive.allowUnreachable = %s", tc.value)
		s.Equal(tc.want.IsTrue(), cfg.Extensions.WorktreeConfig,
			"extensions.worktreeConfig = %s", tc.value)
	}
}
