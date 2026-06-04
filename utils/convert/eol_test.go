package convert

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCRLFToLFConverter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  []byte
		output []byte
	}{
		{
			input:  []byte("CRLF\r\nto\r\nLF"),
			output: []byte("CRLF\nto\nLF"),
		},
		{
			input:  []byte("\r\n\r\n\r\n\r\n\r\n"),
			output: []byte("\n\n\n\n\n"),
		},
	}

	for _, test := range tests {
		t.Run(string(test.input), func(t *testing.T) {
			t.Parallel()
			buf := bytes.NewBuffer(nil)
			conv := NewLFWriter(buf)

			n, err := conv.Write(test.input)
			require.NoError(t, err)

			assert.Equal(t, len(test.input), n)
			assert.Equal(t, test.output, buf.Bytes())
		})
	}
}

func TestLFToCRLFConverter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  []byte
		output []byte
	}{
		{
			input:  []byte("LF\nto\nCRLF"),
			output: []byte("LF\r\nto\r\nCRLF"),
		},
		{
			input:  []byte("\n\n\n\n\n"),
			output: []byte("\r\n\r\n\r\n\r\n\r\n"),
		},
	}

	for _, test := range tests {
		t.Run(string(test.input), func(t *testing.T) {
			t.Parallel()
			buf := bytes.NewBuffer(nil)
			conv := NewCRLFWriter(buf)

			n, err := conv.Write(test.input)
			require.NoError(t, err)

			assert.Equal(t, len(test.input), n)
			assert.Equal(t, test.output, buf.Bytes())
		})
	}
}
