package git

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage/memory"
)

func (s *RemoteSuite) TestTransportProtocolDefault() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"https://example.com/foo.git"}})
	s.Equal(config.DefaultProtocolVersion, r.transportProtocol())
}

func (s *RemoteSuite) TestTransportProtocolFromConfig() {
	st := memory.NewStorage()
	cfg, err := st.Config()
	s.Require().NoError(err)
	cfg.Protocol.Version = protocol.V2
	s.Require().NoError(st.SetConfig(cfg))

	r := NewRemote(st, &config.RemoteConfig{Name: "foo", URLs: []string{"https://example.com/foo.git"}})
	s.Equal(protocol.V2, r.transportProtocol())
}

func TestFetchRefPrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		specs    []config.RefSpec
		tags     plumbing.TagMode
		noHead   bool
		contains []string
		excludes []string
		wantNil  bool
	}{
		{
			name:  "short-name source expands to full-name candidates",
			specs: []config.RefSpec{"master:foo"},
			tags:  plumbing.NoTags,
			// A v2 server prefix-matches the full refname, so "master" alone
			// never matches refs/heads/master; the expansion must cover it.
			contains: []string{"master", "refs/heads/master", "refs/tags/master", "HEAD"},
		},
		{
			name:     "qualified source is preserved",
			specs:    []config.RefSpec{"refs/heads/main:refs/heads/main"},
			tags:     plumbing.NoTags,
			contains: []string{"refs/heads/main", "HEAD"},
		},
		{
			name:     "wildcard source uses the literal prefix",
			specs:    []config.RefSpec{"refs/heads/*:refs/remotes/origin/*"},
			tags:     plumbing.NoTags,
			contains: []string{"refs/heads/", "HEAD"},
		},
		{
			name:    "leading wildcard requests the full advertisement",
			specs:   []config.RefSpec{"*:refs/remotes/origin/*"},
			tags:    plumbing.NoTags,
			wantNil: true,
		},
		{
			name:     "tag following advertises refs/tags/",
			specs:    []config.RefSpec{"master:foo"},
			tags:     plumbing.TagFollowing,
			contains: []string{"refs/heads/master", "refs/tags/", "HEAD"},
		},
		{
			name:     "HEAD left out when not wanted",
			specs:    []config.RefSpec{"+refs/heads/main:refs/remotes/origin/main"},
			tags:     plumbing.AllTags,
			noHead:   true,
			contains: []string{"refs/heads/main", "refs/tags/"},
			excludes: []string{"HEAD"},
		},
		{
			name:     "HEAD source requests HEAD even when not wanted",
			specs:    []config.RefSpec{"+HEAD:refs/remotes/origin/HEAD"},
			tags:     plumbing.NoTags,
			noHead:   true,
			contains: []string{"refs/heads/", "HEAD"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := fetchRefPrefixes(tc.specs, tc.tags, !tc.noHead)
			if tc.wantNil {
				assert.Nil(t, got)
				return
			}
			for _, want := range tc.contains {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tc.excludes {
				assert.NotContains(t, got, notWant)
			}
		})
	}
}
