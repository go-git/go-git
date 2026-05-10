package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseConfigBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want OptBool
	}{
		// Truthy values, case-insensitive.
		{"true", OptBoolTrue},
		{"True", OptBoolTrue},
		{"TRUE", OptBoolTrue},
		{"yes", OptBoolTrue},
		{"Yes", OptBoolTrue},
		{"YES", OptBoolTrue},
		{"on", OptBoolTrue},
		{"On", OptBoolTrue},
		{"ON", OptBoolTrue},
		{"1", OptBoolTrue},

		// Falsy values, case-insensitive.
		{"false", OptBoolFalse},
		{"False", OptBoolFalse},
		{"FALSE", OptBoolFalse},
		{"no", OptBoolFalse},
		{"No", OptBoolFalse},
		{"NO", OptBoolFalse},
		{"off", OptBoolFalse},
		{"Off", OptBoolFalse},
		{"OFF", OptBoolFalse},
		{"0", OptBoolFalse},

		// Arbitrary integers, mirroring upstream's !!v on git_parse_int.
		{"2", OptBoolTrue},
		{"-1", OptBoolTrue},
		{"010", OptBoolTrue}, // strconv.Atoi parses decimal, not octal.
		{"99999", OptBoolTrue},
		{"-0", OptBoolFalse},
		{"0000", OptBoolFalse},
		{"+1", OptBoolTrue},

		// Anything else stays unset, including empty / unrecognised.
		{"", OptBoolUnset},
		{"maybe", OptBoolUnset},
		{"t", OptBoolUnset}, // strconv.ParseBool would accept this; Git does not.
		{"T", OptBoolUnset},
		{"f", OptBoolUnset},
		{"F", OptBoolUnset},
		{"0x1", OptBoolUnset},      // not decimal
		{"1.5", OptBoolUnset},      // not an integer
		{"  true  ", OptBoolUnset}, // trimming is the caller's job.
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got := parseConfigBool(tc.in)
			assert.Equal(t, tc.want, got, "parseConfigBool(%q)", tc.in)
		})
	}
}

func TestUnmarshalProtectNTFSAcceptsGitStyleBooleans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  OptBool
	}{
		{"true", OptBoolTrue},
		{"yes", OptBoolTrue},
		{"on", OptBoolTrue},
		{"1", OptBoolTrue},
		{"false", OptBoolFalse},
		{"no", OptBoolFalse},
		{"off", OptBoolFalse},
		{"0", OptBoolFalse},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()

			body := []byte("[core]\n\tprotectNTFS = " + tc.value + "\n\tprotectHFS = " + tc.value + "\n")
			cfg := NewConfig()
			err := cfg.Unmarshal(body)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, cfg.Core.ProtectNTFS, "protectNTFS")
			assert.Equal(t, tc.want, cfg.Core.ProtectHFS, "protectHFS")
		})
	}
}

func TestUnmarshalProtectNTFSUnsetForUnrecognised(t *testing.T) {
	t.Parallel()

	body := []byte("[core]\n\tprotectNTFS = maybe\n\tprotectHFS = maybe\n")
	cfg := NewConfig()
	err := cfg.Unmarshal(body)
	assert.NoError(t, err)
	assert.Equal(t, OptBoolUnset, cfg.Core.ProtectNTFS,
		"unrecognised value should leave protectNTFS unset")
	assert.Equal(t, OptBoolUnset, cfg.Core.ProtectHFS,
		"unrecognised value should leave protectHFS unset")
}
