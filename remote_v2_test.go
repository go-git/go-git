package git

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestFetchRefPrefixes(t *testing.T) {
	t.Parallel()

	specs := func(ss ...string) []config.RefSpec {
		out := make([]config.RefSpec, len(ss))
		for i, s := range ss {
			out[i] = config.RefSpec(s)
		}
		return out
	}

	cases := []struct {
		name  string
		specs []config.RefSpec
		tags  plumbing.TagMode
		want  []string
	}{
		{
			name:  "wildcard with all tags",
			specs: specs("+refs/heads/*:refs/remotes/origin/*"),
			tags:  plumbing.AllTags,
			want:  []string{"refs/heads/", "refs/tags/", "HEAD"},
		},
		{
			name:  "exact ref, no tags",
			specs: specs("refs/heads/main:refs/remotes/origin/main"),
			tags:  plumbing.NoTags,
			want:  []string{"refs/heads/main", "HEAD"},
		},
		{
			name:  "tag following adds refs/tags/",
			specs: specs("+refs/heads/*:refs/remotes/origin/*"),
			tags:  plumbing.TagFollowing,
			want:  []string{"refs/heads/", "refs/tags/", "HEAD"},
		},
		{
			name:  "no refspecs requests full advertisement",
			specs: nil,
			tags:  plumbing.AllTags,
			want:  nil,
		},
		{
			name:  "exact SHA1 source requests full advertisement",
			specs: specs("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:refs/heads/x"),
			tags:  plumbing.AllTags,
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, fetchRefPrefixes(tc.specs, tc.tags))
		})
	}
}

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
