package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing/hash"
)

func TestObjectFormatAliases(t *testing.T) {
	t.Parallel()

	assert.IsType(t, hash.ObjectFormat(""), SHA256)
	assert.Equal(t, hash.SHA256, SHA256)
	assert.Equal(t, hash.SHA1, ObjectFormat(SHA1))
	assert.Equal(t, hash.UnsetObjectFormat, ObjectFormat(UnsetObjectFormat))
	assert.Equal(t, hash.DefaultObjectFormat, ObjectFormat(DefaultObjectFormat))
	assert.Equal(t, hash.SHA1Size, SHA1Size)
	assert.Equal(t, hash.SHA256Size, SHA256Size)
	assert.Equal(t, hash.SHA1HexSize, SHA1HexSize)
	assert.Equal(t, hash.SHA256HexSize, SHA256HexSize)
	assert.ErrorIs(t, ErrInvalidObjectFormat, hash.ErrInvalidObjectFormat)
}
