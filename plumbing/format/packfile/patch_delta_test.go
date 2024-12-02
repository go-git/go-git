package packfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecodeLEB128(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		want     uint
		wantRest []byte
	}{
		{
			name:     "single byte, small number",
			input:    []byte{0x01, 0xFF},
			want:     1,
			wantRest: []byte{0xFF},
		},
		{
			name:     "single byte, max value without continuation",
			input:    []byte{0x7F, 0xFF},
			want:     127,
			wantRest: []byte{0xFF},
		},
		{
			name:     "two bytes",
			input:    []byte{0x80, 0x01, 0xFF},
			want:     128,
			wantRest: []byte{0xFF},
		},
		{
			name:     "two bytes, larger number",
			input:    []byte{0xFF, 0x01, 0xFF},
			want:     255,
			wantRest: []byte{0xFF},
		},
		{
			name:     "three bytes",
			input:    []byte{0x80, 0x80, 0x01, 0xFF},
			want:     16384,
			wantRest: []byte{0xFF},
		},
		{
			name:     "empty remaining bytes",
			input:    []byte{0x01},
			want:     1,
			wantRest: []byte{},
		},
		{
			name:     "empty input",
			input:    []byte{},
			want:     0,
			wantRest: []byte{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotNum, gotRest := decodeLEB128(tc.input)
			assert.Equal(t, tc.want, gotNum, "decoded number mismatch")
			assert.Equal(t, tc.wantRest, gotRest, "remaining bytes mismatch")
		})
	}
}
