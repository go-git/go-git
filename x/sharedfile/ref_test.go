package sharedfile

import (
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeFile is a ReadAtCloser whose Close is observable.
type fakeFile struct{ closes atomic.Int64 }

func (f *fakeFile) ReadAt(_ []byte, _ int64) (int, error) { return 0, io.EOF }
func (f *fakeFile) Read(_ []byte) (int, error)            { return 0, io.EOF }
func (f *fakeFile) Close() error                          { f.closes.Add(1); return nil }

func TestRefHoldsFileOpenPastGrace(t *testing.T) {
	t.Parallel()
	ff := &fakeFile{}
	sf := New(func() (ReadAtCloser, error) { return ff, nil }, time.Millisecond)

	ref, err := sf.Ref()
	require.NoError(t, err)
	require.NotNil(t, ref.File())

	// A held Ref keeps the file open regardless of the grace window.
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, int64(0), ff.closes.Load(), "held Ref must keep the file open")

	require.NoError(t, ref.Close())
	require.NoError(t, ref.Close(), "Close is idempotent")
}
