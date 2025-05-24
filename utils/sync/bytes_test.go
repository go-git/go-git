package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAndPutBytesBuffer(t *testing.T) {
	buf := GetBytesBuffer()
	require.NotNil(t, buf)

	initialLen := buf.Len()
	buf.Grow(initialLen * 2)
	grownLen := buf.Len()

	PutBytesBuffer(buf)

	buf = GetBytesBuffer()
	assert.Equal(t, buf.Len(), grownLen)
	PutBytesBuffer(buf)

	buf2 := GetBytesBuffer()
	assert.Equal(t, buf2.Len(), initialLen)
	PutBytesBuffer(buf2)
}

func TestGetAndPutByteSlice(t *testing.T) {
	slice := GetByteSlice()
	require.NotNil(t, slice)

	wantLen := 32 * 1024
	assert.Len(t, *slice, wantLen)

	truncated := *slice
	truncated = truncated[:0]

	PutByteSlice(&truncated)

	// ensure the truncated slice is not returned
	slice = GetByteSlice()
	assert.Len(t, *slice, wantLen)

	values := *slice
	values[0] = 1

	PutByteSlice(&values)

	slice = GetByteSlice()
	values = *slice
	assert.Len(t, values, wantLen)
	// ensure that values
	assert.Equal(t, uint8(0), values[0])
}
