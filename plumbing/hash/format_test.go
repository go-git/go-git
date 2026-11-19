package hash

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestObjectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		format  ObjectFormat
		str     string
		size    int
		hexSize int
	}{
		{format: SHA1, str: "sha1", size: 20, hexSize: 40},
		{format: SHA256, str: "sha256", size: 32, hexSize: 64},
		{format: UnsetObjectFormat, str: "", size: 20, hexSize: 40},
		{format: ObjectFormat("sha512"), str: "sha512", size: 20, hexSize: 40},
	}

	for _, tc := range tests {
		t.Run(tc.str, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.str, tc.format.String())
			assert.Equal(t, tc.size, tc.format.Size())
			assert.Equal(t, tc.hexSize, tc.format.HexSize())
		})
	}

	assert.Equal(t, SHA1, DefaultObjectFormat)
}
