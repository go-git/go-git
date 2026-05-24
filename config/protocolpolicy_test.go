package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The allow keys are checked when a scheme's policy is looked up, as in
// Git, so a value that is unknown or empty does not stop the config from
// loading, and only fails the lookups that reach it.
func TestProtocolPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		raw     string
		scheme  string
		want    string
		wantErr string
	}{
		{name: "unset", raw: "", scheme: "file", want: ""},
		{name: "global", raw: "[protocol]\n\tallow = ALWAYS\n", scheme: "file", want: ProtocolAlways},
		{name: "per-scheme wins", raw: "[protocol]\n\tallow = never\n[protocol \"file\"]\n\tallow = User\n", scheme: "file", want: ProtocolUser},
		{name: "other scheme falls back", raw: "[protocol]\n\tallow = never\n[protocol \"file\"]\n\tallow = user\n", scheme: "ssh", want: ProtocolNever},
		{name: "unknown global", raw: "[protocol]\n\tallow = sometimes\n", scheme: "file", wantErr: "unknown value for config 'protocol.allow': sometimes"},
		{name: "empty global", raw: "[protocol]\n\tallow =\n", scheme: "file", wantErr: "unknown value for config 'protocol.allow': "},
		{name: "unknown per-scheme", raw: "[protocol \"file\"]\n\tallow = perhaps\n", scheme: "file", wantErr: "unknown value for config 'protocol.file.allow': perhaps"},
		{name: "empty per-scheme", raw: "[protocol \"file\"]\n\tallow =\n", scheme: "file", wantErr: "unknown value for config 'protocol.file.allow': "},
		{name: "bad per-scheme not looked up", raw: "[protocol \"foo\"]\n\tallow = bogus\n", scheme: "file", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := NewConfig()
			require.NoError(t, cfg.Unmarshal([]byte(tc.raw)))
			got, err := cfg.ProtocolPolicy(tc.scheme)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// An empty allow value is set, not absent, and keeps being so through a
// marshal and a merge of config scopes.
func TestProtocolPolicyKeepsEmptyValue(t *testing.T) {
	t.Parallel()

	cfg := NewConfig()
	require.NoError(t, cfg.Unmarshal([]byte("[protocol]\n\tallow =\n")))
	require.NotNil(t, cfg.Protocol.Allow)

	buf, err := cfg.Marshal()
	require.NoError(t, err)
	got := NewConfig()
	require.NoError(t, got.Unmarshal(buf))
	merged := Merge(NewConfig(), got)
	_, err = merged.ProtocolPolicy("file")
	require.Error(t, err)
}
