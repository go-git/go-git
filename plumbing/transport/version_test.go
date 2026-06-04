package transport

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
)

func TestDiscoverVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected protocol.Version
		wantErr  bool
	}{
		{
			name:     "version 1",
			input:    "version 1\n",
			expected: protocol.V1,
		},
		{
			name:     "version 2",
			input:    "version 2\n",
			expected: protocol.V2,
		},
		{
			name:     "no version prefix",
			input:    "git-upload-pack /project.git\n",
			expected: protocol.V0,
		},
		{
			name:     "unknown version",
			input:    "version 999\n",
			expected: protocol.V0,
		},
		{
			name:     "empty input",
			input:    "",
			expected: protocol.V0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if tt.input != "" {
				pktline.WriteString(&buf, tt.input)
			}

			r := bufio.NewReader(&buf)
			version, err := DiscoverVersion(r)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expected, version)
		})
	}
}

func TestProtocolVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected protocol.Version
	}{
		{
			name:     "version 1",
			input:    "version=1",
			expected: protocol.V1,
		},
		{
			name:     "version 2",
			input:    "version=2",
			expected: protocol.V2,
		},
		{
			name:     "version with other parameters",
			input:    "hello:version=2:side-band-64k",
			expected: protocol.V2,
		},
		{
			name:     "multiple versions takes highest",
			input:    "version=1:version=2",
			expected: protocol.V2,
		},
		{
			name:     "no version parameter",
			input:    "side-band-64k:thin-pack",
			expected: protocol.V0,
		},
		{
			name:     "unknown version",
			input:    "version=999",
			expected: protocol.V0,
		},
		{
			name:     "empty string",
			input:    "",
			expected: protocol.V0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			version := ProtocolVersion(tt.input)
			assert.Equal(t, tt.expected, version)
		})
	}
}
