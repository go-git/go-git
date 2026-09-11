package config

import (
	"bytes"
	"testing"

	oldconfig "github.com/go-git/go-git/v6/config"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

const benchFixture = `[core]
	repositoryformatversion = 0
	filemode = true
	bare = false
	logallrefupdates = true
	worktree = /home/user/project
	commentchar = auto
[user]
	name = Test User
	email = test@example.com
[remote "origin"]
	url = https://github.com/go-git/go-git
	fetch = +refs/heads/*:refs/remotes/origin/*
[remote "upstream"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
[branch "main"]
	remote = origin
	merge = refs/heads/main
[branch "develop"]
	remote = origin
	merge = refs/heads/develop
[pack]
	window = 10
[init]
	defaultbranch = main
`

type benchRemote struct {
	URL   []string `gitconfig:"url,multivalue"`
	Fetch []string `gitconfig:"fetch,multivalue"`
}

type benchBranch struct {
	Remote string `gitconfig:"remote"`
	Merge  string `gitconfig:"merge"`
}

type benchCore struct {
	RepositoryFormatVersion int    `gitconfig:"repositoryformatversion"`
	FileMode                bool   `gitconfig:"filemode"`
	Bare                    bool   `gitconfig:"bare"`
	LogAllRefUpdates        bool   `gitconfig:"logallrefupdates"`
	Worktree                string `gitconfig:"worktree"`
	CommentChar             string `gitconfig:"commentchar"`
}

type benchUser struct {
	Name  string `gitconfig:"name"`
	Email string `gitconfig:"email"`
}

type benchPack struct {
	Window int `gitconfig:"window"`
}

type benchInit struct {
	DefaultBranch string `gitconfig:"defaultbranch"`
}

type benchConfig struct {
	Core     benchCore               `gitconfig:"core"`
	User     benchUser               `gitconfig:"user"`
	Remotes  map[string]*benchRemote `gitconfig:"remote,subsection"`
	Branches map[string]*benchBranch `gitconfig:"branch,subsection"`
	Pack     benchPack               `gitconfig:"pack"`
	Init     benchInit               `gitconfig:"init"`
}

func BenchmarkUnmarshal_XConfig(b *testing.B) {
	raw := format.New()
	if err := format.NewDecoder(bytes.NewReader([]byte(benchFixture))).Decode(raw); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		var cfg benchConfig
		if err := Unmarshal(raw, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_OldConfig(b *testing.B) {
	data := []byte(benchFixture)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cfg := oldconfig.NewConfig()
		if err := cfg.Unmarshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalWithParse_XConfig(b *testing.B) {
	data := []byte(benchFixture)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		raw := format.New()
		if err := format.NewDecoder(bytes.NewReader(data)).Decode(raw); err != nil {
			b.Fatal(err)
		}
		var cfg benchConfig
		if err := Unmarshal(raw, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalWithParse_OldConfig(b *testing.B) {
	data := []byte(benchFixture)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cfg := oldconfig.NewConfig()
		if err := cfg.Unmarshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal_XConfig(b *testing.B) {
	raw := format.New()
	if err := format.NewDecoder(bytes.NewReader([]byte(benchFixture))).Decode(raw); err != nil {
		b.Fatal(err)
	}
	var cfg benchConfig
	if err := Unmarshal(raw, &cfg); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		out := format.New()
		if err := Marshal(cfg, out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal_OldConfig(b *testing.B) {
	cfg := oldconfig.NewConfig()
	if err := cfg.Unmarshal([]byte(benchFixture)); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := cfg.Marshal(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_XConfig(b *testing.B) {
	data := []byte(benchFixture)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		raw := format.New()
		if err := format.NewDecoder(bytes.NewReader(data)).Decode(raw); err != nil {
			b.Fatal(err)
		}
		var cfg benchConfig
		if err := Unmarshal(raw, &cfg); err != nil {
			b.Fatal(err)
		}
		out := format.New()
		if err := Marshal(cfg, out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_OldConfig(b *testing.B) {
	data := []byte(benchFixture)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cfg := oldconfig.NewConfig()
		if err := cfg.Unmarshal(data); err != nil {
			b.Fatal(err)
		}
		if _, err := cfg.Marshal(); err != nil {
			b.Fatal(err)
		}
	}
}
