package convert

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"

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

	readers := map[string]func(io.Reader) io.Reader{
		"plain":   func(r io.Reader) io.Reader { return r },
		"onebyte": iotest.OneByteReader,
		"half":    iotest.HalfReader,
		"dataerr": iotest.DataErrReader,
	}

	for name, wrap := range readers {
		for idx, test := range tests {
			t.Run(fmt.Sprintf("%s/#%d %q", name, idx, test.input), func(t *testing.T) {
				t.Parallel()
				r := wrap(bytes.NewReader(test.input))

				stat, err := GetStat(r)
				require.NoError(t, err)

				assert.Equal(t, test.expected, stat)
			})
		}
	}
}

func TestGetStatDataWithEOF(t *testing.T) {
	t.Parallel()

	stat, err := GetStat(iotest.DataErrReader(strings.NewReader("a\x00")))
	require.NoError(t, err)

	assert.Equal(t, Stat{Printable: 1, NUL: 1}, stat)
	assert.True(t, stat.IsBinary())
}

func TestGetStatReadError(t *testing.T) {
	t.Parallel()

	_, err := GetStat(iotest.ErrReader(io.ErrUnexpectedEOF))
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
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
