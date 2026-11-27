package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAndPutByteSlice(t *testing.T) {
	t.Parallel()
	slice := GetByteSlice()
	require.NotNil(t, slice)

	wantLen := 32 * 1024
	assert.Len(t, *slice, wantLen)

	PutByteSlice(slice)
}
