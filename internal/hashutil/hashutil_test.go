package hashutil

import (
	"crypto/sha1"
	"crypto/sha256"
	"testing"

	"github.com/pjbgf/sha1cd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		format hash.ObjectFormat
		size   int
	}{
		{hash.UnsetObjectFormat, sha1.Size},
		{hash.SHA1, sha1.Size},
		{hash.SHA256, sha256.Size},
	}

	for _, tc := range tests {
		t.Run(string(tc.format), func(t *testing.T) {
			t.Parallel()

			h, err := New(tc.format)
			require.NoError(t, err)
			assert.Equal(t, tc.size, h.Size())
			require.NoError(t, Check(tc.format))
		})
	}
}

func TestNewUsesRegisteredSHA1(t *testing.T) {
	t.Parallel()

	h, err := New(hash.SHA1)
	require.NoError(t, err)
	assert.IsType(t, sha1cd.New(), h)
}

func TestNewUnsupportedFormat(t *testing.T) {
	t.Parallel()

	h, err := New(hash.ObjectFormat("sha512"))
	require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
	assert.ErrorContains(t, err, "sha512")
	assert.Nil(t, h)

	err = Check(hash.ObjectFormat("sha512"))
	require.ErrorIs(t, err, hash.ErrUnsupportedHashFunction)
	assert.ErrorContains(t, err, "sha512")
}
