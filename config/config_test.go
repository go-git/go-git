package config

import (
	. "gopkg.in/check.v1"
	"github.com/go-git/go-git/v5/plumbing"
	format "github.com/go-git/go-git/v5/plumbing/format/config"
)

type ConfigSuite struct{}

var _ = Suite(&ConfigSuite{})

func (s *ConfigSuite) TestUnmarshal(c *C) {
	input := []byte(`[core]
        bare = true
		worktree = foo
		commentchar = bar
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
[remote "win-local"]
		url = X:\\Git\\
[submodule "qux"]
        path = qux
        url = https://github.com/foo/qux.git
		branch = bar
[branch "master"]
        remote = origin
        merge = refs/heads/master
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	c.Assert(err, IsNil)

	c.Assert(cfg.Core.IsBare, Equals, true)
	c.Assert(cfg.Core.Worktree, Equals, "foo")
	c.Assert(cfg.Core.CommentChar, Equals, "bar")
	c.Assert(cfg.Pack.Window, Equals, uint(20))
	c.Assert(cfg.Remotes, HasLen, 3)
	c.Assert(cfg.Remotes["origin"].Name, Equals, "origin")
	c.Assert(cfg.Remotes["origin"].URLs, DeepEquals, []string{"git@github.com:mcuadros/go-git.git"})
	c.Assert(cfg.Remotes["origin"].Fetch, DeepEquals, []RefSpec{"+refs/heads/*:refs/remotes/origin/*"})
	c.Assert(cfg.Remotes["alt"].Name, Equals, "alt")
	c.Assert(cfg.Remotes["alt"].URLs, DeepEquals, []string{"git@github.com:mcuadros/go-git.git", "git@github.com:src-d/go-git.git"})
	c.Assert(cfg.Remotes["alt"].Fetch, DeepEquals, []RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*:refs/remotes/origin/pull/*"})
	c.Assert(cfg.Remotes["win-local"].Name, Equals, "win-local")
	c.Assert(cfg.Remotes["win-local"].URLs, DeepEquals, []string{"X:\\Git\\"})
	c.Assert(cfg.Submodules, HasLen, 1)
	c.Assert(cfg.Submodules["qux"].Name, Equals, "qux")
	c.Assert(cfg.Submodules["qux"].URL, Equals, "https://github.com/foo/qux.git")
	c.Assert(cfg.Submodules["qux"].Branch, Equals, "bar")
	c.Assert(cfg.Branches["master"].Remote, Equals, "origin")
	c.Assert(cfg.Branches["master"].Merge, Equals, plumbing.ReferenceName("refs/heads/master"))
}

func (s *ConfigSuite) TestMergedUnmarshal(c *C) {
	localInput := []byte(`[core]
        bare = true
		worktree = foo
		commentchar = bar
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
[remote "win-local"]
		url = X:\\Git\\
[submodule "qux"]
        path = qux
        url = https://github.com/foo/qux.git
		branch = bar
[branch "master"]
        remote = origin
        merge = refs/heads/master
[user]
		name = Override
`)

	globalInput := []byte(`
[user]
		name = Soandso
		email = soandso@example.com
[core]
        editor = nvim
[push]
        default = simple
`)

	cfg := NewConfig()

	err := cfg.UnmarshalScoped(format.LocalScope, localInput)
	c.Assert(err, IsNil)

	err = cfg.UnmarshalScoped(format.GlobalScope, globalInput)
	c.Assert(err, IsNil)

	c.Assert(cfg.Core.IsBare, Equals, true)
	c.Assert(cfg.Core.Worktree, Equals, "foo")
	c.Assert(cfg.Core.CommentChar, Equals, "bar")
	c.Assert(cfg.Pack.Window, Equals, uint(20))
	c.Assert(cfg.User.Name.Value(), Equals, "Override")
	c.Assert(cfg.User.Email.Value(), Equals, "soandso@example.com")
	c.Assert(cfg.Remotes, HasLen, 3)
	c.Assert(cfg.Remotes["origin"].Name, Equals, "origin")
	c.Assert(cfg.Remotes["origin"].URLs, DeepEquals, []string{"git@github.com:mcuadros/go-git.git"})
	c.Assert(cfg.Remotes["origin"].Fetch, DeepEquals, []RefSpec{"+refs/heads/*:refs/remotes/origin/*"})
	c.Assert(cfg.Remotes["alt"].Name, Equals, "alt")
	c.Assert(cfg.Remotes["alt"].URLs, DeepEquals, []string{"git@github.com:mcuadros/go-git.git", "git@github.com:src-d/go-git.git"})
	c.Assert(cfg.Remotes["alt"].Fetch, DeepEquals, []RefSpec{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*:refs/remotes/origin/pull/*"})
	c.Assert(cfg.Remotes["win-local"].Name, Equals, "win-local")
	c.Assert(cfg.Remotes["win-local"].URLs, DeepEquals, []string{"X:\\Git\\"})
	c.Assert(cfg.Submodules, HasLen, 1)
	c.Assert(cfg.Submodules["qux"].Name, Equals, "qux")
	c.Assert(cfg.Submodules["qux"].URL, Equals, "https://github.com/foo/qux.git")
	c.Assert(cfg.Submodules["qux"].Branch, Equals, "bar")
	c.Assert(cfg.Branches["master"].Remote, Equals, "origin")
	c.Assert(cfg.Branches["master"].Merge, Equals, plumbing.ReferenceName("refs/heads/master"))
	c.Assert(cfg.Merged.Section("user").Option("name"), Equals, "Override")
	c.Assert(cfg.Merged.Section("user").Option("email"), Equals, "soandso@example.com")
	c.Assert(cfg.Merged.Section("push").Option("default"), Equals, "simple")
}

func (s *ConfigSuite) TestMarshal(c *C) {
	output := []byte(`[core]
	bare = true
	worktree = bar
[pack]
	window = 20
[remote "alt"]
	url = git@github.com:mcuadros/go-git.git
	url = git@github.com:src-d/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	fetch = +refs/pull/*:refs/remotes/origin/pull/*
[remote "origin"]
	url = git@github.com:mcuadros/go-git.git
[remote "win-local"]
	url = "X:\\Git\\"
[submodule "qux"]
	url = https://github.com/foo/qux.git
[branch "master"]
	remote = origin
	merge = refs/heads/master
`)

	cfg := NewConfig()
	cfg.Core.IsBare = true
	cfg.Core.Worktree = "bar"
	cfg.Pack.Window = 20
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

	cfg.Submodules["qux"] = &Submodule{
		Name: "qux",
		URL:  "https://github.com/foo/qux.git",
	}

	cfg.Branches["master"] = &Branch{
		Name:   "master",
		Remote: "origin",
		Merge:  "refs/heads/master",
	}

	b, err := cfg.Marshal()
	c.Assert(err, IsNil)

	c.Assert(string(b), Equals, string(output))
}

func (s *ConfigSuite) TestMergedMarshal(c *C) {
	localOutput := []byte(`[custom]
	key = value
[core]
	bare = true
	worktree = bar
[pack]
	window = 20
[remote "alt"]
	url = git@github.com:mcuadros/go-git.git
	url = git@github.com:src-d/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	fetch = +refs/pull/*:refs/remotes/origin/pull/*
[remote "origin"]
	url = git@github.com:mcuadros/go-git.git
[remote "win-local"]
	url = "X:\\Git\\"
[submodule "qux"]
	url = https://github.com/foo/qux.git
[branch "master"]
	remote = origin
	merge = refs/heads/master
[user]
	name = Override
`)

	globalOutput := []byte(`[core]
	editor = nvim
[push]
	default = simple
[user]
	name = Soandso
	email = soandso@example.com
	useConfigOnly = true
`)

	cfg := NewConfig()

	cfg.Core.IsBare = true
	cfg.Core.Worktree = "bar"
	cfg.Pack.Window = 20

	cfg.User.Name.Set(format.GlobalScope, "Soandso")
	cfg.User.Email.Set(format.GlobalScope, "soandso@example.com")

	cfg.User.Name.Set(format.LocalScope, "Override")

	uco := true
	cfg.User.UseConfigOnly.Set(format.GlobalScope, uco)
	uco = false // make sure that this doesn't change the value due to pointer shenanigans

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

	cfg.Submodules["qux"] = &Submodule{
		Name: "qux",
		URL:  "https://github.com/foo/qux.git",
	}

	cfg.Branches["master"] = &Branch{
		Name:   "master",
		Remote: "origin",
		Merge:  "refs/heads/master",
	}

	cfg.Merged.GlobalConfig().Section("core").AddOption("editor", "nvim")
	cfg.Merged.LocalConfig().Section("custom").SetOption("key", "value")
	cfg.Merged.GlobalConfig().Section("push").AddOption("default", "simple")


	localBytes, err := cfg.Marshal()
	c.Assert(err, IsNil)
	c.Assert(cfg.Merged.Section("user").Option("name"), Equals, "Override")
	c.Assert(string(localBytes), Equals, string(localOutput))

	globalBytes, err := cfg.MarshalScope(format.GlobalScope)
	c.Assert(err, IsNil)
	c.Assert(string(globalBytes), Equals, string(globalOutput))

	systemBytes, err := cfg.MarshalScope(format.SystemScope)
	c.Assert(err, IsNil)
	c.Assert(string(systemBytes), Equals, "")
}

func (s *ConfigSuite) TestUnmarshalMarshal(c *C) {
	input := []byte(`[core]
	bare = true
	worktree = foo
	custom = ignored
[pack]
	window = 20
[remote "origin"]
	url = git@github.com:mcuadros/go-git.git
	fetch = +refs/heads/*:refs/remotes/origin/*
	mirror = true
[remote "win-local"]
	url = "X:\\Git\\"
[branch "master"]
	remote = origin
	merge = refs/heads/master
`)

	cfg := NewConfig()
	err := cfg.Unmarshal(input)
	c.Assert(err, IsNil)

	output, err := cfg.Marshal()
	c.Assert(err, IsNil)
	c.Assert(string(output), DeepEquals, string(input))
}

func (s *ConfigSuite) TestValidateConfig(c *C) {
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

	c.Assert(config.Validate(), IsNil)
}

func (s *ConfigSuite) TestValidateInvalidRemote(c *C) {
	config := &Config{
		Remotes: map[string]*RemoteConfig{
			"foo": {Name: "foo"},
		},
	}

	c.Assert(config.Validate(), Equals, ErrRemoteConfigEmptyURL)
}

func (s *ConfigSuite) TestValidateInvalidRemoteKey(c *C) {
	config := &Config{
		Remotes: map[string]*RemoteConfig{
			"bar": {Name: "foo"},
		},
	}

	c.Assert(config.Validate(), Equals, ErrInvalid)
}

func (s *ConfigSuite) TestRemoteConfigValidateMissingURL(c *C) {
	config := &RemoteConfig{Name: "foo"}
	c.Assert(config.Validate(), Equals, ErrRemoteConfigEmptyURL)
}

func (s *ConfigSuite) TestRemoteConfigValidateMissingName(c *C) {
	config := &RemoteConfig{}
	c.Assert(config.Validate(), Equals, ErrRemoteConfigEmptyName)
}

func (s *ConfigSuite) TestRemoteConfigValidateDefault(c *C) {
	config := &RemoteConfig{Name: "foo", URLs: []string{"http://foo/bar"}}
	c.Assert(config.Validate(), IsNil)

	fetch := config.Fetch
	c.Assert(fetch, HasLen, 1)
	c.Assert(fetch[0].String(), Equals, "+refs/heads/*:refs/remotes/foo/*")
}

func (s *ConfigSuite) TestValidateInvalidBranchKey(c *C) {
	config := &Config{
		Branches: map[string]*Branch{
			"foo": {
				Name:   "bar",
				Remote: "origin",
				Merge:  plumbing.ReferenceName("refs/heads/bar"),
			},
		},
	}

	c.Assert(config.Validate(), Equals, ErrInvalid)
}

func (s *ConfigSuite) TestValidateInvalidBranch(c *C) {
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

	c.Assert(config.Validate(), Equals, errBranchInvalidMerge)
}

func (s *ConfigSuite) TestRemoteConfigDefaultValues(c *C) {
	config := NewConfig()

	c.Assert(config.Remotes, HasLen, 0)
	c.Assert(config.Branches, HasLen, 0)
	c.Assert(config.Submodules, HasLen, 0)
	c.Assert(config.Raw, NotNil)
	c.Assert(config.Pack.Window, Equals, DefaultPackWindow)
}
