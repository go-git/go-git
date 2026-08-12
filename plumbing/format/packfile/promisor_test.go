package packfile

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/memory"
)

// packWriterOnly writes packfiles but cannot record them as promisor packs —
// the shape that must be refused, since it is how an unmarked pack of
// deliberately absent objects reaches disk.
type packWriterOnly struct {
	*memory.Storage
}

func (packWriterOnly) PackfileWriter() (io.WriteCloser, error) {
	return nil, assert.AnError
}

// promisorCapable records promisor packs, so it is safe for a filtered fetch.
type promisorCapable struct {
	packWriterOnly
}

func (promisorCapable) PromisorPackfileWriter(string) (io.WriteCloser, error) {
	return nil, assert.AnError
}

func TestSupportsPromisorPacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		st   storer.Storer
		want bool
	}{
		{
			name: "records promisor packs",
			st:   promisorCapable{packWriterOnly{memory.NewStorage()}},
			want: true,
		},
		{
			name: "writes no packfiles, so nothing to mark",
			st:   memory.NewStorage(),
			want: true,
		},
		{
			name: "writes packfiles it cannot mark",
			st:   packWriterOnly{memory.NewStorage()},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, SupportsPromisorPacks(tc.st))
		})
	}
}

// TestUpdatePromisorObjectStorageRefusesUnmarkablePack pins the fail-closed
// behaviour: rather than silently writing an unmarked pack, the write is refused.
func TestUpdatePromisorObjectStorageRefusesUnmarkablePack(t *testing.T) {
	t.Parallel()

	err := UpdatePromisorObjectStorage(packWriterOnly{memory.NewStorage()},
		bytes.NewReader(nil), "")
	require.ErrorIs(t, err, ErrPromisorPacksUnsupported)
}
