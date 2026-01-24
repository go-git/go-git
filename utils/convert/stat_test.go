package convert

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    []byte
		expected Stat
	}{
		{
			input: []byte("ABCDEFG"),
			expected: Stat{
				Printable: 7,
			},
		},
		{
			input: []byte("\r\n\r\n\r\n"),
			expected: Stat{
				CRLF: 3,
			},
		},
		{
			input: []byte("\r\r\n"),
			expected: Stat{
				LoneCR: 1,
				CRLF:   1,
			},
		},
		{
			input: []byte("\r\n\r"),
			expected: Stat{
				LoneCR: 1,
				CRLF:   1,
			},
		},
		{
			input: []byte("NUL\x00"),
			expected: Stat{
				Printable: 3,
				NUL:       1,
			},
		},
		{
			input: []byte{127}, // DEL
			expected: Stat{
				NonPrintable: 1,
			},
		},
		{
			input: []byte{'\n', '\032'}, // LF, EOF
			expected: Stat{
				LoneLF:       1,
				NonPrintable: 0,
			},
		},
	}

	for idx, test := range tests {
		t.Run(fmt.Sprintf("#%d %s", idx, test.input), func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader(test.input)

			stat, err := GetStat(r)
			require.NoError(t, err)

			assert.Equal(t, test.expected, stat)
		})
	}
}

func TestIsBinary(t *testing.T) {
	t.Parallel()
	stat := Stat{}
	assert.False(t, stat.IsBinary())

	stat = Stat{NUL: 1}
	assert.True(t, stat.IsBinary())

	stat = Stat{LoneCR: 1}
	assert.True(t, stat.IsBinary())

	stat = Stat{Printable: 1, NonPrintable: 1}
	assert.True(t, stat.IsBinary())

	stat = Stat{Printable: 1 << 7, NonPrintable: 1}
	assert.False(t, stat.IsBinary())
}
