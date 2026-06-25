package git

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
)

type PorcelainConfigSuite struct {
	suite.Suite
}

func TestPorcelainConfigSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(PorcelainConfigSuite))
}

func porcelainConfigWith(setup func(*config.Config)) *config.Config {
	c := config.NewConfig()
	if setup != nil {
		setup(c)
	}
	return c
}

func addURLConfig(cfg *config.Config, u *URL) {
	setSubsection(cfg, urlSection, u.Name, u.marshal())
}

func (s *PorcelainConfigSuite) TestUnmarshal() {
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

	cfg := config.NewConfig()
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
	remotes := remoteConfigs(cfg)
	s.Len(remotes, 4)
	s.Equal("origin", remotes["origin"].Name)
	s.Equal([]string{"git@github.com:mcuadros/go-git.git"}, remotes["origin"].URLs)
	s.Equal([]plumbing.RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, remotes["origin"].Fetch)
	s.Equal("alt", remotes["alt"].Name)
	s.Equal([]string{"git@github.com:mcuadros/go-git.git", "git@github.com:src-d/go-git.git"}, remotes["alt"].URLs)
	s.Equal([]plumbing.RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*:refs/remotes/origin/pull/*"}, remotes["alt"].Fetch)
	s.Equal("win-local", remotes["win-local"].Name)
	s.Equal([]string{"X:\\Git\\"}, remotes["win-local"].URLs)
	s.Equal([]string{"ssh://git@github.com/kostyay/go-git.git"}, remotes["insteadOf"].URLs)
	submodules := submoduleConfigs(cfg)
	s.Len(submodules, 1)
	s.Equal("qux", submodules["qux"].Name)
	s.Equal("https://github.com/foo/qux.git", submodules["qux"].URL)
	s.Equal("bar", submodules["qux"].Branch)
	branches := branchConfigs(cfg)
	s.Equal("origin", branches["master"].Remote)
	s.Equal(plumbing.ReferenceName("refs/heads/master"), branches["master"].Merge)
	s.Equal("Add support for branch description.\n\nEdit branch description: git branch --edit-description\n", branches["master"].Description)
	s.Equal("main", cfg.Init().DefaultBranch)
}

func (s *PorcelainConfigSuite) TestMarshal() {
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

	cfg := config.NewConfig()
	cfg.SetBare(true)
	cfg.SetWorktree("bar")
	cfg.SetAutoCRLF("true")
	cfg.SetHooksPath("custom-hooks")
	cfg.SetPackWindow(20)
	cfg.SetInitDefaultBranch("main")
	setRemoteConfig(cfg, &RemoteConfig{
		Name: "origin",
		URLs: []string{"git@github.com:mcuadros/go-git.git"},
	})

	setRemoteConfig(cfg, &RemoteConfig{
		Name:  "alt",
		URLs:  []string{"git@github.com:mcuadros/go-git.git", "git@github.com:src-d/go-git.git"},
		Fetch: []plumbing.RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*:refs/remotes/origin/pull/*"},
	})

	setRemoteConfig(cfg, &RemoteConfig{
		Name: "win-local",
		URLs: []string{"X:\\Git\\"},
	})

	setRemoteConfig(cfg, &RemoteConfig{
		Name: "insteadOf",
		URLs: []string{"https://github.com/kostyay/go-git.git"},
	})

	setSubmoduleConfig(cfg, &SubmoduleConfig{
		Name: "qux",
		URL:  "https://github.com/foo/qux.git",
	})

	setBranchConfig(cfg, &Branch{
		Name:        "master",
		Remote:      "origin",
		Merge:       "refs/heads/master",
		Description: "Add support for branch description.\n\nEdit branch description: git branch --edit-description\n",
	})

	addURLConfig(cfg, &URL{
		Name:       "ssh://git@github.com/",
		InsteadOfs: []string{"https://github.com/"},
	})

	b, err := cfg.Marshal()
	s.NoError(err)

	s.Equal(string(output), string(b))
}

func (s *PorcelainConfigSuite) TestValidateConfig() {
	cfg := porcelainConfigWith(func(c *config.Config) {
		setRemoteConfig(c, &RemoteConfig{
			Name: "bar",
			URLs: []string{"http://foo/bar"},
		})
		setBranchConfig(c, &Branch{
			Name: "bar",
		})
		setBranchConfig(c, &Branch{
			Name:   "foo",
			Remote: "origin",
			Merge:  plumbing.ReferenceName("refs/heads/foo"),
		})
	})

	s.NoError(cfg.Validate())
}

func (s *PorcelainConfigSuite) TestRemoteConfigValidateMissingURL() {
	cfg := &RemoteConfig{Name: "foo"}
	s.ErrorIs(cfg.Validate(), ErrRemoteConfigEmptyURL)
}

func (s *PorcelainConfigSuite) TestRemoteConfigValidateMissingName() {
	cfg := &RemoteConfig{}
	s.ErrorIs(cfg.Validate(), ErrRemoteConfigEmptyName)
}

func (s *PorcelainConfigSuite) TestRemoteConfigValidateDefault() {
	cfg := &RemoteConfig{Name: "foo", URLs: []string{"http://foo/bar"}}
	s.NoError(cfg.Validate())

	fetch := cfg.Fetch
	s.Len(fetch, 1)
	s.Equal("+refs/heads/*:refs/remotes/foo/*", fetch[0].String())
}

func (s *PorcelainConfigSuite) TestValidateBranchNonRefsPrefix() {
	cfg := porcelainConfigWith(func(c *config.Config) {
		setBranchConfig(c, &Branch{
			Name:   "bar",
			Remote: "origin",
			Merge:  plumbing.ReferenceName("refs/heads/bar"),
		})
		setBranchConfig(c, &Branch{
			Name:   "foo",
			Remote: "origin",
			Merge:  plumbing.ReferenceName("baz"),
		})
	})

	s.NoError(cfg.Validate())
	s.NoError(branchConfig(cfg, "foo").Validate())
}

func (s *PorcelainConfigSuite) TestRemoteConfigDefaultValues() {
	cfg := config.NewConfig()

	s.Len(remoteConfigs(cfg), 0)
	s.Len(branchConfigs(cfg), 0)
	s.Len(submoduleConfigs(cfg), 0)
	s.NotNil(cfg.Raw)
	s.Equal(config.DefaultPackWindow, cfg.Pack().Window)
}

func (s *PorcelainConfigSuite) TestRemoveUrlOptions() {
	buf := []byte(`
[remote "alt"]
	url = git@github.com:mcuadros/go-git.git
	url = git@github.com:src-d/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	fetch = +refs/pull/*:refs/remotes/origin/pull/*`)

	cfg := config.NewConfig()
	err := cfg.Unmarshal(buf)
	s.NoError(err)
	s.Len(remoteConfigs(cfg), 1)
	alt := remoteConfig(cfg, "alt")
	alt.URLs = []string{}
	setRemoteConfig(cfg, alt)

	buf, err = cfg.Marshal()
	s.NoError(err)
	if strings.Contains(string(buf), "url") {
		s.Fail("config should not contain any url sections")
	}
	s.NoError(err)
}

func (s *PorcelainConfigSuite) TestUnmarshalRemotes() {
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

	cfg := config.NewConfig()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	remotes := remoteConfigs(cfg)
	s.Equal("https://git.sr.ht/~mcepl/go-git", remotes["origin"].URLs[0])
	s.Equal("git@git.sr.ht:~mcepl/go-git.git", remotes["origin"].URLs[1])
}

func (s *PorcelainConfigSuite) TestUnmarshalRemotesUnnamedFirst() {
	input := []byte(`
[remote ""]
  url = https://github.com/CLBRITTON2/go-git.git
  fetch = +refs/heads/*:refs/remotes/origin/*
[remote "upstream"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
	`)

	cfg := config.NewConfig()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	remotes := remoteConfigs(cfg)
	unnamedRemote, ok := remotes[""]
	s.True(ok, "Expected unnamed remote to be present")
	s.Equal([]string{"https://github.com/CLBRITTON2/go-git.git"}, unnamedRemote.URLs)
	s.Equal([]plumbing.RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, unnamedRemote.Fetch)

	namedRemote, ok := remotes["upstream"]
	s.True(ok, "Expected named remote 'upstream' to be present")
	s.Equal([]string{"https://github.com/go-git/go-git.git"}, namedRemote.URLs)
	s.Equal([]plumbing.RefSpec{"+refs/heads/*:refs/remotes/upstream/*"}, namedRemote.Fetch)
}

func (s *PorcelainConfigSuite) TestUnmarshalRemotesNamedFirst() {
	input := []byte(`
[remote "upstream"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
[remote ""]
  url = https://github.com/CLBRITTON2/go-git.git
  fetch = +refs/heads/*:refs/remotes/origin/*
	`)

	cfg := config.NewConfig()
	err := cfg.Unmarshal(input)
	s.NoError(err)

	remotes := remoteConfigs(cfg)
	namedRemote, ok := remotes["upstream"]
	s.True(ok, "Expected a named remote 'upstream' to be present")
	s.Equal([]string{"https://github.com/go-git/go-git.git"}, namedRemote.URLs)
	s.Equal([]plumbing.RefSpec{"+refs/heads/*:refs/remotes/upstream/*"}, namedRemote.Fetch)

	unnamedRemote, ok := remotes[""]
	s.True(ok, "Expected an unnamed remote to be present")
	s.Equal([]string{"https://github.com/CLBRITTON2/go-git.git"}, unnamedRemote.URLs)
	s.Equal([]plumbing.RefSpec{"+refs/heads/*:refs/remotes/origin/*"}, unnamedRemote.Fetch)
}
