package commitgraph

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncodeFileHeaderRejectsTooManyChunks(t *testing.T) {
	t.Parallel()
	// The on-disk format stores num_chunks as a uint8, so any encoder
	// path that tries to emit more than 255 chunk types would silently
	// truncate. The encoder must reject the configuration at write time.
	e := NewEncoder(&bytes.Buffer{})

	err := e.encodeFileHeader(256)
	assert.ErrorIs(t, err, ErrTooManyChunks)
}
