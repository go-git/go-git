package idxfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriterOnHeaderCapsPrealloc(t *testing.T) {
	t.Parallel()

	declared := uint32(4 * maxObjectsPrealloc)

	w := new(Writer)
	assert.NoError(t, w.OnHeader(declared))

	assert.Equal(t, declared, w.count)
	assert.LessOrEqual(t, cap(w.objects), maxObjectsPrealloc)
}
