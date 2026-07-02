package sharedfile

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetireWithNoRefsClosesImmediately(t *testing.T) {
	t.Parallel()
	ff := &fakeFile{}
	sf := New(func() (ReadAtCloser, error) { return ff, nil }, time.Second)

	ref, err := sf.Ref()
	require.NoError(t, err)
	require.NoError(t, ref.Close()) // refs back to 0

	require.NoError(t, sf.Retire())
	require.Equal(t, int64(1), ff.closes.Load(), "Retire closes when idle")
	require.True(t, sf.IsClosed())

	_, err = sf.Ref()
	require.ErrorIs(t, err, fs.ErrClosed, "Retire seals against new Ref")
}

func TestRetireDefersCloseWhileReferenced(t *testing.T) {
	t.Parallel()
	ff := &fakeFile{}
	sf := New(func() (ReadAtCloser, error) { return ff, nil }, time.Second)

	ref, err := sf.Ref()
	require.NoError(t, err)

	require.NoError(t, sf.Retire())
	require.Equal(t, int64(0), ff.closes.Load(), "Retire must not close under a live Ref")
	require.False(t, sf.IsClosed(), "not closed until the last Ref drops")
	require.NotNil(t, ref.File())

	_, err = sf.Ref()
	require.ErrorIs(t, err, fs.ErrClosed, "sealed: no new capture, no reopen")

	require.NoError(t, ref.Close())
	require.Equal(t, int64(1), ff.closes.Load(), "closes on last Release")
	require.True(t, sf.IsClosed())
}

func TestRetireIsIdempotentAndSafeAfterClose(t *testing.T) {
	t.Parallel()
	ff := &fakeFile{}
	sf := New(func() (ReadAtCloser, error) { return ff, nil }, time.Second)
	require.NoError(t, sf.Retire())
	require.NoError(t, sf.Retire(), "idempotent")
	require.NoError(t, sf.Close(), "Close after Retire returns nil")
	require.Equal(t, int64(0), ff.closes.Load(), "never opened, nothing to close")
}

func TestRetireDoesNotReopenUnlikeReleaseNow(t *testing.T) {
	t.Parallel()
	opens := 0
	ff := &fakeFile{}
	sf := New(func() (ReadAtCloser, error) { opens++; return ff, nil }, time.Second)

	ref, err := sf.Ref()
	require.NoError(t, err)
	require.Equal(t, 1, opens)
	require.NoError(t, ref.Close())

	require.NoError(t, sf.Retire())
	_, err = sf.Ref()
	require.True(t, errors.Is(err, fs.ErrClosed))
	require.Equal(t, 1, opens, "Retire never reopens")
}

func TestRetireAfterReleaseNowClosesExactlyOnce(t *testing.T) {
	t.Parallel()
	ff := &fakeFile{}
	sf := New(func() (ReadAtCloser, error) { return ff, nil }, time.Hour)
	ref, err := sf.Ref()
	require.NoError(t, err)
	require.NoError(t, sf.ReleaseNow()) // latch immediateClose, refs=1
	require.NoError(t, sf.Retire())     // seal, refs=1
	require.NoError(t, ref.Close())     // last Release: sealed branch fires
	require.Equal(t, int64(1), ff.closes.Load(), "closes exactly once")
	require.True(t, sf.IsClosed())
}
