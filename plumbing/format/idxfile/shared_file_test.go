package idxfile

import (
	"bytes"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackingFile struct {
	*bytes.Reader
	closed atomic.Bool
}

func (f *trackingFile) Read(p []byte) (int, error)              { return f.Reader.Read(p) }
func (f *trackingFile) Seek(off int64, w int) (int64, error)    { return f.Reader.Seek(off, w) }
func (f *trackingFile) ReadAt(p []byte, off int64) (int, error) { return f.Reader.ReadAt(p, off) }
func (f *trackingFile) Close() error {
	f.closed.Store(true)
	return nil
}

// newTestSharedFile creates a sharedFile with zero grace period for
// deterministic test behavior.
func newTestSharedFile(opener openFileFunc) *sharedFile {
	sf := newSharedFile(opener)
	sf.gracePeriod = 0
	return sf
}

func TestSharedFileAcquireRelease(t *testing.T) {
	t.Parallel()
	var opens atomic.Int32
	tf := &trackingFile{Reader: bytes.NewReader([]byte("hello"))}
	opener := func() (ReadAtCloser, error) {
		opens.Add(1)
		tf.closed.Store(false)
		return tf, nil
	}

	sf := newTestSharedFile(opener)

	r1, err := sf.acquire()
	require.NoError(t, err)
	assert.Equal(t, int32(1), opens.Load())
	assert.False(t, tf.closed.Load())

	r2, err := sf.acquire()
	require.NoError(t, err)
	assert.Equal(t, int32(1), opens.Load()) // no second open
	assert.Same(t, r1, r2)                  // same underlying file

	// Release doesn't close (refs still > 0).
	sf.release()
	assert.False(t, tf.closed.Load())

	// Release closes (refs == 0).
	sf.release()
	assert.True(t, tf.closed.Load())

	// Next acquire reopens.
	_, err = sf.acquire()
	require.NoError(t, err)
	assert.Equal(t, int32(2), opens.Load())
	sf.release()
}

func TestSharedFileCloseWithActiveReaders(t *testing.T) {
	t.Parallel()
	tf := &trackingFile{Reader: bytes.NewReader([]byte("hello"))}
	opener := func() (ReadAtCloser, error) {
		tf.closed.Store(false)
		return tf, nil
	}

	sf := newTestSharedFile(opener)

	// Acquire a reference.
	r, err := sf.acquire()
	require.NoError(t, err)
	require.NotNil(t, r)

	// Close while reader is active — file stays open.
	err = sf.Close()
	require.NoError(t, err)
	assert.False(t, tf.closed.Load())

	// Further acquires are rejected.
	_, err = sf.acquire()
	assert.ErrorIs(t, err, errSharedFileClosed)

	// Active reader can still read.
	buf := make([]byte, 5)
	n, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	// Release closes the file.
	sf.release()
	assert.True(t, tf.closed.Load())
}

func TestSharedFileConcurrent(t *testing.T) {
	t.Parallel()
	data := []byte("concurrent test data for shared file")
	opener := func() (ReadAtCloser, error) {
		return &trackingFile{Reader: bytes.NewReader(data)}, nil
	}

	sf := newTestSharedFile(opener)
	defer sf.Close()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			r, err := sf.acquire()
			if err != nil {
				return
			}
			defer sf.release()

			buf := make([]byte, 4)
			_, _ = r.ReadAt(buf, 0)
		}()
	}

	wg.Wait()

	// After all goroutines finish, file should be closed (refs == 0).
	sf.mu.Lock()
	assert.Nil(t, sf.file)
	assert.Equal(t, 0, sf.refs)
	sf.mu.Unlock()
}

func TestSharedFileCloseIdempotent(t *testing.T) {
	t.Parallel()
	opener := func() (ReadAtCloser, error) {
		return &trackingFile{Reader: bytes.NewReader(nil)}, nil
	}

	sf := newTestSharedFile(opener)

	// Acquire and release so file gets opened then closed.
	_, err := sf.acquire()
	require.NoError(t, err)
	sf.release()

	// Multiple Close calls should be safe.
	assert.NoError(t, sf.Close())
	assert.NoError(t, sf.Close())
}

func TestSharedopenFileFuncError(t *testing.T) {
	t.Parallel()
	opener := func() (ReadAtCloser, error) {
		return nil, io.ErrUnexpectedEOF
	}

	sf := newTestSharedFile(opener)

	_, err := sf.acquire()
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestSharedFileReleaseUnderflow(t *testing.T) {
	t.Parallel()
	opener := func() (ReadAtCloser, error) {
		return &trackingFile{Reader: bytes.NewReader(nil)}, nil
	}

	sf := newTestSharedFile(opener)

	assert.NotPanics(t, func() {
		sf.release()
	})

	_, err := sf.acquire()
	require.NoError(t, err)
	sf.release()
	assert.NotPanics(t, func() {
		sf.release()
	})
}

func TestSharedFileGracePeriod(t *testing.T) {
	t.Parallel()
	var opens atomic.Int32
	tf := &trackingFile{Reader: bytes.NewReader([]byte("grace"))}
	opener := func() (ReadAtCloser, error) {
		opens.Add(1)
		tf.closed.Store(false)
		return tf, nil
	}

	sf := newSharedFile(opener)
	sf.gracePeriod = 50 * time.Millisecond

	// First acquire + release: file stays open during grace period.
	_, err := sf.acquire()
	require.NoError(t, err)
	sf.release()
	assert.False(t, tf.closed.Load(), "file should stay open during grace period")

	// Second acquire within grace period reuses the same FD.
	_, err = sf.acquire()
	require.NoError(t, err)
	assert.Equal(t, int32(1), opens.Load(), "should reuse FD within grace period")
	sf.release()

	// Wait for grace period to expire.
	time.Sleep(100 * time.Millisecond)
	assert.True(t, tf.closed.Load(), "file should close after grace period")

	// Next acquire reopens.
	_, err = sf.acquire()
	require.NoError(t, err)
	assert.Equal(t, int32(2), opens.Load())
	sf.release()

	_ = sf.Close()
}

func TestSharedFileGracePeriodResetByAcquire(t *testing.T) {
	t.Parallel()
	var opens atomic.Int32
	tf := &trackingFile{Reader: bytes.NewReader([]byte("reset"))}
	opener := func() (ReadAtCloser, error) {
		opens.Add(1)
		tf.closed.Store(false)
		return tf, nil
	}

	sf := newSharedFile(opener)
	sf.gracePeriod = 80 * time.Millisecond

	_, err := sf.acquire()
	require.NoError(t, err)
	sf.release()
	assert.False(t, tf.closed.Load())

	// Acquire within grace period reuses the FD and resets the timer.
	time.Sleep(50 * time.Millisecond)
	_, err = sf.acquire()
	require.NoError(t, err)
	assert.Equal(t, int32(1), opens.Load(), "should reuse FD, not reopen")
	sf.release()

	// 50ms into the new 80ms grace period: file still open.
	time.Sleep(50 * time.Millisecond)
	assert.False(t, tf.closed.Load(), "new grace period hasn't expired")

	// After the new grace period expires: file closed.
	time.Sleep(50 * time.Millisecond)
	assert.True(t, tf.closed.Load())
	assert.Equal(t, int32(1), opens.Load())

	_ = sf.Close()
}

func TestSharedFileGracePeriodCancelledByClose(t *testing.T) {
	t.Parallel()
	tf := &trackingFile{Reader: bytes.NewReader([]byte("cancel"))}
	opener := func() (ReadAtCloser, error) {
		tf.closed.Store(false)
		return tf, nil
	}

	sf := newSharedFile(opener)
	sf.gracePeriod = time.Minute // long grace period

	_, err := sf.acquire()
	require.NoError(t, err)
	sf.release()
	assert.False(t, tf.closed.Load())

	// Close cancels the grace timer and closes immediately.
	require.NoError(t, sf.Close())
	assert.True(t, tf.closed.Load())
}
