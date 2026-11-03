package config

import (
	"bytes"
	"testing"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

func parseRaw(t *testing.T, input string) *format.Config {
	t.Helper()
	raw := format.New()
	if err := format.NewDecoder(bytes.NewReader([]byte(input))).Decode(raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func encodeRaw(t *testing.T, raw *format.Config) string {
	t.Helper()
	var buf bytes.Buffer
	if err := format.NewEncoder(&buf).Encode(raw); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestUnmarshalBasicSection(t *testing.T) {
	t.Parallel()
	raw := parseRaw(t, "[core]\n\tbare = true\n\tworktree = /path/to/wt\n\tcommentchar = \"#\"\n")

	type Core struct {
		Bare        bool   `gitconfig:"bare"`
		Worktree    string `gitconfig:"worktree"`
		CommentChar string `gitconfig:"commentchar"`
	}
	type Config struct {
		Core Core `gitconfig:"core"`
	}

	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	if !cfg.Core.Bare {
		t.Error("expected Core.Bare = true")
	}
	if cfg.Core.Worktree != "/path/to/wt" {
		t.Errorf("expected Core.Worktree = /path/to/wt, got %q", cfg.Core.Worktree)
	}
	if cfg.Core.CommentChar != "#" {
		t.Errorf("expected Core.CommentChar = #, got %q", cfg.Core.CommentChar)
	}
}

func TestUnmarshalBoolVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"yes", true},
		{"on", true},
		{"1", true},
		{"false", false},
		{"False", false},
		{"FALSE", false},
		{"no", false},
		{"off", false},
		{"0", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			raw := parseRaw(t, "[core]\n\tbare = "+tt.input+"\n")

			type Core struct {
				Bare bool `gitconfig:"bare"`
			}
			type Config struct {
				Core Core `gitconfig:"core"`
			}

			var cfg Config
			if err := Unmarshal(raw, &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.Core.Bare != tt.want {
				t.Errorf("input %q: got %v, want %v", tt.input, cfg.Core.Bare, tt.want)
			}
		})
	}
}

func TestUnmarshalPointerBool(t *testing.T) {
	t.Parallel()
	type Index struct {
		SkipHash *bool `gitconfig:"skiphash"`
	}
	type Config struct {
		Index Index `gitconfig:"index"`
	}

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		var cfg Config
		if err := Unmarshal(parseRaw(t, "[index]\n"), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Index.SkipHash != nil {
			t.Error("expected nil for absent key")
		}
	})

	t.Run("true", func(t *testing.T) {
		t.Parallel()
		var cfg Config
		if err := Unmarshal(parseRaw(t, "[index]\n\tskiphash = true\n"), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Index.SkipHash == nil || !*cfg.Index.SkipHash {
			t.Error("expected *true")
		}
	})

	t.Run("false", func(t *testing.T) {
		t.Parallel()
		var cfg Config
		if err := Unmarshal(parseRaw(t, "[index]\n\tskiphash = false\n"), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.Index.SkipHash == nil || *cfg.Index.SkipHash {
			t.Error("expected *false")
		}
	})
}

func TestUnmarshalIntegers(t *testing.T) {
	t.Parallel()
	raw := parseRaw(t, "[pack]\n\twindow = 15\n\tdepth = 50\n")

	type Pack struct {
		Window uint `gitconfig:"window"`
		Depth  int  `gitconfig:"depth"`
	}
	type Config struct {
		Pack Pack `gitconfig:"pack"`
	}

	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Pack.Window != 15 {
		t.Errorf("expected window=15, got %d", cfg.Pack.Window)
	}
	if cfg.Pack.Depth != 50 {
		t.Errorf("expected depth=50, got %d", cfg.Pack.Depth)
	}
}

func TestUnmarshalDefault(t *testing.T) {
	t.Parallel()
	type Core struct {
		CommentChar string `gitconfig:"commentchar" gitconfigDefault:"#"`
		Window      int    `gitconfig:"window" gitconfigDefault:"10"`
	}
	type Config struct {
		Core Core `gitconfig:"core"`
	}

	var cfg Config
	if err := Unmarshal(parseRaw(t, "[core]\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Core.CommentChar != "#" {
		t.Errorf("expected default #, got %q", cfg.Core.CommentChar)
	}
	if cfg.Core.Window != 10 {
		t.Errorf("expected default 10, got %d", cfg.Core.Window)
	}
}

func TestUnmarshalSubsectionMap(t *testing.T) {
	t.Parallel()
	raw := parseRaw(t, `[remote "origin"]
	url = https://github.com/go-git/go-git
	fetch = +refs/heads/*:refs/remotes/origin/*
[remote "upstream"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
`)

	type Remote struct {
		URL   []string `gitconfig:"url,multivalue"`
		Fetch []string `gitconfig:"fetch,multivalue"`
	}
	type Config struct {
		Remotes map[string]*Remote `gitconfig:"remote,subsection"`
	}

	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Remotes) != 2 {
		t.Fatalf("expected 2 remotes, got %d", len(cfg.Remotes))
	}

	origin := cfg.Remotes["origin"]
	if origin == nil {
		t.Fatal("missing remote origin")
	}
	if len(origin.URL) != 1 || origin.URL[0] != "https://github.com/go-git/go-git" {
		t.Errorf("unexpected origin URL: %v", origin.URL)
	}

	upstream := cfg.Remotes["upstream"]
	if upstream == nil {
		t.Fatal("missing remote upstream")
	}
}

func TestUnmarshalMultipleURLs(t *testing.T) {
	t.Parallel()
	raw := parseRaw(t, `[remote "origin"]
	url = https://github.com/go-git/go-git
	url = git@github.com:go-git/go-git.git
`)

	type Remote struct {
		URL []string `gitconfig:"url,multivalue"`
	}
	type Config struct {
		Remotes map[string]*Remote `gitconfig:"remote,subsection"`
	}

	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	origin := cfg.Remotes["origin"]
	if origin == nil {
		t.Fatal("missing remote origin")
	}
	if len(origin.URL) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(origin.URL))
	}
	if origin.URL[0] != "https://github.com/go-git/go-git" {
		t.Errorf("unexpected URL[0]: %s", origin.URL[0])
	}
	if origin.URL[1] != "git@github.com:go-git/go-git.git" {
		t.Errorf("unexpected URL[1]: %s", origin.URL[1])
	}
}

func TestUnmarshalSingleSubsection(t *testing.T) {
	t.Parallel()
	raw := parseRaw(t, `[gpg]
	format = ssh
[gpg "ssh"]
	program = /usr/bin/ssh-keygen
	allowedsignersfile = ~/.ssh/allowed_signers
`)

	type SSHConfig struct {
		Program            string `gitconfig:"program"`
		AllowedSignersFile string `gitconfig:"allowedsignersfile"`
	}
	type GPGConfig struct {
		Format string `gitconfig:"format"`
	}
	type Config struct {
		GPG    GPGConfig  `gitconfig:"gpg"`
		GPGSSH *SSHConfig `gitconfig:"gpg,subsection" gitconfigSub:"ssh"`
	}

	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.GPG.Format != "ssh" {
		t.Errorf("expected format=ssh, got %q", cfg.GPG.Format)
	}
	if cfg.GPGSSH == nil {
		t.Fatal("expected GPGSSH to be set")
	}
	if cfg.GPGSSH.Program != "/usr/bin/ssh-keygen" {
		t.Errorf("unexpected program: %q", cfg.GPGSSH.Program)
	}
	if cfg.GPGSSH.AllowedSignersFile != "~/.ssh/allowed_signers" {
		t.Errorf("unexpected allowedSignersFile: %q", cfg.GPGSSH.AllowedSignersFile)
	}
}

func TestUnmarshalCustomType(t *testing.T) {
	t.Parallel()
	raw := parseRaw(t, "[http]\n\tfollowredirects = initial\n")

	type HTTP struct {
		FollowRedirects *customEnum `gitconfig:"followredirects"`
	}
	type Config struct {
		HTTP HTTP `gitconfig:"http"`
	}

	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.FollowRedirects == nil {
		t.Fatal("expected non-nil")
	}
	if string(*cfg.HTTP.FollowRedirects) != "initial" {
		t.Errorf("expected initial, got %q", *cfg.HTTP.FollowRedirects)
	}
}

type customEnum string

func (c *customEnum) UnmarshalGitConfig(data []byte) error {
	*c = customEnum(data)
	return nil
}

func (c customEnum) MarshalGitConfig() (string, error) {
	return string(c), nil
}

func TestUnmarshalCaseInsensitive(t *testing.T) {
	t.Parallel()
	raw := parseRaw(t, "[Core]\n\tBare = true\n")

	type Core struct {
		Bare bool `gitconfig:"bare"`
	}
	type Config struct {
		Core Core `gitconfig:"core"`
	}

	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Core.Bare {
		t.Error("expected case-insensitive match for section and key")
	}
}

func TestMarshalBasic(t *testing.T) {
	t.Parallel()
	type Core struct {
		Bare     bool   `gitconfig:"bare"`
		Worktree string `gitconfig:"worktree"`
	}
	type Config struct {
		Core Core `gitconfig:"core"`
	}

	cfg := Config{Core: Core{Bare: true, Worktree: "/path"}}
	raw := format.New()
	if err := Marshal(cfg, raw); err != nil {
		t.Fatal(err)
	}

	s := encodeRaw(t, raw)
	if !contains(s, "bare = true") {
		t.Errorf("expected bare = true in output:\n%s", s)
	}
	if !contains(s, "worktree = /path") {
		t.Errorf("expected worktree = /path in output:\n%s", s)
	}
}

func TestMarshalOmitempty(t *testing.T) {
	t.Parallel()
	type Core struct {
		Bare     bool   `gitconfig:"bare,omitempty"`
		Worktree string `gitconfig:"worktree,omitempty"`
	}
	type Config struct {
		Core Core `gitconfig:"core"`
	}

	cfg := Config{Core: Core{Bare: false, Worktree: ""}}
	raw := format.New()
	if err := Marshal(cfg, raw); err != nil {
		t.Fatal(err)
	}

	s := encodeRaw(t, raw)
	if contains(s, "bare") {
		t.Error("expected bare to be omitted")
	}
	if contains(s, "worktree") {
		t.Error("expected worktree to be omitted")
	}
}

func TestMarshalPointerBool(t *testing.T) {
	t.Parallel()
	type Index struct {
		SkipHash *bool `gitconfig:"skiphash"`
	}
	type Config struct {
		Index Index `gitconfig:"index"`
	}

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		raw := format.New()
		if err := Marshal(Config{}, raw); err != nil {
			t.Fatal(err)
		}
		if contains(encodeRaw(t, raw), "skiphash") {
			t.Error("expected nil pointer to be omitted")
		}
	})

	t.Run("true", func(t *testing.T) {
		t.Parallel()
		v := true
		raw := format.New()
		if err := Marshal(Config{Index: Index{SkipHash: &v}}, raw); err != nil {
			t.Fatal(err)
		}
		if !contains(encodeRaw(t, raw), "skiphash = true") {
			t.Error("expected skiphash = true")
		}
	})

	t.Run("false", func(t *testing.T) {
		t.Parallel()
		v := false
		raw := format.New()
		if err := Marshal(Config{Index: Index{SkipHash: &v}}, raw); err != nil {
			t.Fatal(err)
		}
		if !contains(encodeRaw(t, raw), "skiphash = false") {
			t.Error("expected skiphash = false")
		}
	})
}

func TestMarshalSubsectionMap(t *testing.T) {
	t.Parallel()
	type Remote struct {
		URL   []string `gitconfig:"url,multivalue"`
		Fetch []string `gitconfig:"fetch,multivalue"`
	}
	type Config struct {
		Remotes map[string]*Remote `gitconfig:"remote,subsection"`
	}

	cfg := Config{
		Remotes: map[string]*Remote{
			"origin": {
				URL:   []string{"https://github.com/go-git/go-git"},
				Fetch: []string{"+refs/heads/*:refs/remotes/origin/*"},
			},
		},
	}

	raw := format.New()
	if err := Marshal(cfg, raw); err != nil {
		t.Fatal(err)
	}

	s := encodeRaw(t, raw)
	if !contains(s, `[remote "origin"]`) {
		t.Errorf("expected remote origin section:\n%s", s)
	}
	if !contains(s, "url = https://github.com/go-git/go-git") {
		t.Errorf("expected url:\n%s", s)
	}
}

func TestMarshalSingleSubsection(t *testing.T) {
	t.Parallel()
	type SSHConfig struct {
		Program string `gitconfig:"program"`
	}
	type GPGConfig struct {
		Format string `gitconfig:"format"`
	}
	type Config struct {
		GPG    GPGConfig  `gitconfig:"gpg"`
		GPGSSH *SSHConfig `gitconfig:"gpg,subsection" gitconfigSub:"ssh"`
	}

	cfg := Config{
		GPG:    GPGConfig{Format: "ssh"},
		GPGSSH: &SSHConfig{Program: "/usr/bin/ssh-keygen"},
	}

	raw := format.New()
	if err := Marshal(cfg, raw); err != nil {
		t.Fatal(err)
	}

	s := encodeRaw(t, raw)
	if !contains(s, "format = ssh") {
		t.Errorf("expected format:\n%s", s)
	}
	if !contains(s, `[gpg "ssh"]`) {
		t.Errorf("expected gpg ssh subsection:\n%s", s)
	}
	if !contains(s, "program = /usr/bin/ssh-keygen") {
		t.Errorf("expected program:\n%s", s)
	}
}

func TestMarshalCustomType(t *testing.T) {
	t.Parallel()
	type HTTP struct {
		FollowRedirects customEnum `gitconfig:"followredirects"`
	}
	type Config struct {
		HTTP HTTP `gitconfig:"http"`
	}

	cfg := Config{HTTP: HTTP{FollowRedirects: "initial"}}
	raw := format.New()
	if err := Marshal(cfg, raw); err != nil {
		t.Fatal(err)
	}

	if !contains(encodeRaw(t, raw), "followredirects = initial") {
		t.Error("unexpected output")
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	input := `[core]
	bare = true
	worktree = /path/to/wt
[remote "origin"]
	url = https://github.com/go-git/go-git
	fetch = +refs/heads/*:refs/remotes/origin/*
[remote "upstream"]
	url = https://github.com/go-git/go-git.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
`
	type Remote struct {
		URL   []string `gitconfig:"url,multivalue"`
		Fetch []string `gitconfig:"fetch,multivalue"`
	}
	type Core struct {
		Bare     bool   `gitconfig:"bare"`
		Worktree string `gitconfig:"worktree"`
	}
	type Config struct {
		Core    Core               `gitconfig:"core"`
		Remotes map[string]*Remote `gitconfig:"remote,subsection"`
	}

	raw := parseRaw(t, input)
	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	raw2 := format.New()
	if err := Marshal(cfg, raw2); err != nil {
		t.Fatal(err)
	}

	var cfg2 Config
	if err := Unmarshal(raw2, &cfg2); err != nil {
		t.Fatal(err)
	}

	if cfg2.Core.Bare != cfg.Core.Bare {
		t.Error("round-trip: bare mismatch")
	}
	if cfg2.Core.Worktree != cfg.Core.Worktree {
		t.Error("round-trip: worktree mismatch")
	}
	if len(cfg2.Remotes) != len(cfg.Remotes) {
		t.Errorf("round-trip: remotes count mismatch: %d vs %d", len(cfg2.Remotes), len(cfg.Remotes))
	}
	for name, r := range cfg.Remotes {
		r2 := cfg2.Remotes[name]
		if r2 == nil {
			t.Errorf("round-trip: missing remote %s", name)
			continue
		}
		if len(r2.URL) != len(r.URL) {
			t.Errorf("round-trip: remote %s URL count mismatch", name)
		}
	}
}

func TestRoundTripPreservesUnknown(t *testing.T) {
	t.Parallel()
	input := `[core]
	bare = true
[custom]
	key = value
`
	type Core struct {
		Bare bool `gitconfig:"bare"`
	}
	type Config struct {
		Core Core `gitconfig:"core"`
	}

	raw := parseRaw(t, input)
	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	if err := Marshal(cfg, raw); err != nil {
		t.Fatal(err)
	}

	s := encodeRaw(t, raw)
	if !contains(s, "[custom]") {
		t.Errorf("expected unknown section preserved:\n%s", s)
	}
	if !contains(s, "key = value") {
		t.Errorf("expected unknown key preserved:\n%s", s)
	}
}

func TestUnmarshalInvalidInput(t *testing.T) {
	t.Parallel()
	var cfg struct {
		Core struct {
			Bare bool `gitconfig:"bare"`
		} `gitconfig:"core"`
	}

	err := Unmarshal(parseRaw(t, "[core]\n\tbare = notabool\n"), &cfg)
	if err == nil {
		t.Error("expected error for invalid bool")
	}
}

func TestUnmarshalNonPointerError(t *testing.T) {
	t.Parallel()
	type Config struct{}
	err := Unmarshal(format.New(), Config{})
	if err == nil {
		t.Error("expected error for non-pointer")
	}
}

func TestUnmarshalEmptyConfig(t *testing.T) {
	t.Parallel()
	type Core struct {
		Bare bool `gitconfig:"bare"`
	}
	type Config struct {
		Core Core `gitconfig:"core"`
	}

	var cfg Config
	if err := Unmarshal(format.New(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Core.Bare {
		t.Error("expected false for missing section")
	}
}

func TestUnmarshalSkipField(t *testing.T) {
	t.Parallel()
	raw := parseRaw(t, "[core]\n\tbare = true\n")

	type Core struct {
		Bare    bool   `gitconfig:"bare"`
		Ignored string `gitconfig:"-"`
	}
	type Config struct {
		Core Core `gitconfig:"core"`
	}

	var cfg Config
	if err := Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Core.Ignored != "" {
		t.Error("expected skipped field to be empty")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
