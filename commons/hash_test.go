package commons

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateHash(t *testing.T) {
	assert.Equal(t, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", GitHash("blob", []byte("")))
	assert.Equal(t, "8ab686eafeb1f44702738c8b0f24f2567c36da6d", GitHash("blob", []byte("Hello, World!\n")))
}
