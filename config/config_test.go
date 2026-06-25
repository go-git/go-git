package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

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

func setRawRemote(c *Config, name string, urls ...string) {
	c.Raw.Section("remote").Subsection(name).SetOption("url", urls...)
}

func setRawBranch(c *Config, name string) {
	c.Raw.Section("branch").Subsection(name)
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

func (s *ConfigSuite) TestLoadConfigLocalScope() {
	cfg, err := LoadConfig(LocalScope)
	s.NotNil(err)
	s.Nil(cfg)
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
					setRawRemote(c, "origin", "https://example.com/repo.git")
				}),
				configWith(func(c *Config) {
					// Only the branch subsection is present.
					setRawBranch(c, "main")
				}),
			},
			want: configValueWith(func(c *Config) {
				setRawRemote(c, "origin", "https://example.com/repo.git")
				setRawBranch(c, "main")
			}),
		},
		{
			name: "src empty map preserves dst map",
			input: []*Config{
				configWith(func(c *Config) {
					setRawRemote(c, "origin", "https://example.com/repo.git")
				}),
				configWith(func(_ *Config) {
					// No remote subsection is present.
				}),
			},
			want: configValueWith(func(c *Config) {
				setRawRemote(c, "origin", "https://example.com/repo.git")
			}),
		},
		{
			name: "merge maps with disjoint keys",
			input: []*Config{
				configWith(func(c *Config) {
					setRawRemote(c, "origin", "https://example.com/repo.git")
				}),
				configWith(func(c *Config) {
					setRawRemote(c, "upstream", "https://upstream.com/repo.git")
				}),
			},
			want: configValueWith(func(c *Config) {
				setRawRemote(c, "origin", "https://example.com/repo.git")
				setRawRemote(c, "upstream", "https://upstream.com/repo.git")
			}),
		},
		{
			name: "src map entry overrides dst map entry",
			input: []*Config{
				configWith(func(c *Config) {
					setRawRemote(c, "origin", "https://old.com/repo.git")
				}),
				configWith(func(c *Config) {
					setRawRemote(c, "origin", "https://new.com/repo.git")
				}),
			},
			want: configValueWith(func(c *Config) {
				setRawRemote(c, "origin", "https://new.com/repo.git")
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
