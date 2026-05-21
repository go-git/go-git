package sharedfile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/x/fdpool"
)

// TestSharedFile_Pool_TouchesOnAcquire asserts that every
// successful Acquire updates the pool's LRU — both the first open
// and subsequent acquires while the FD is already held.
func TestSharedFile_Pool_TouchesOnAcquire(t *testing.T) {
	t.Parallel()
	open, _, _ := newOpener(t, []byte("touch"))
	pool := fdpool.New(8)
	sf := NewWithPool(open, time.Hour, pool)
	defer sf.Close()

	_, err := sf.Acquire()
	require.NoError(t, err)
	require.Equal(t, 1, pool.Stats().Active,
		"first Acquire should register the member")

	_, err = sf.Acquire()
	require.NoError(t, err)
	assert.Equal(t, 1, pool.Stats().Active,
		"subsequent Acquire keeps a single LRU entry")
	assert.Equal(t, uint64(1), pool.Stats().Hits,
		"second Touch is an LRU hit")

	sf.Release()
	sf.Release()
}

// TestSharedFile_Pool_KeepsFDOpenOnQuiescence asserts that when a
// pool is wired the FD stays open across refs==0; only the pool's
// eviction (or terminal Close) releases the FD.
func TestSharedFile_Pool_KeepsFDOpenOnQuiescence(t *testing.T) {
	t.Parallel()
	open, opens, handles := newOpener(t, []byte("keep"))
	pool := fdpool.New(8)
	sf := NewWithPool(open, time.Hour, pool)
	defer sf.Close()

	_, err := sf.Acquire()
	require.NoError(t, err)
	sf.Release()
	require.Len(t, *handles, 1)
	assert.False(t, (*handles)[0].closed.Load(),
		"pool-wired SharedFile must keep FD open on refs==0")

	_, err = sf.Acquire()
	require.NoError(t, err)
	assert.Equal(t, int64(1), opens.Load(),
		"re-Acquire must reuse the FD held open by the pool")
	sf.Release()
}

// TestSharedFile_Pool_Close_Forgets asserts that terminal Close
// always invokes Forget on a wired pool, regardless of whether
// references are still active.
func TestSharedFile_Pool_Close_Forgets(t *testing.T) {
	t.Parallel()

	t.Run("ClosesWithNoRefs", func(t *testing.T) {
		t.Parallel()
		open, _, _ := newOpener(t, []byte("noref"))
		pool := fdpool.New(8)
		sf := NewWithPool(open, time.Hour, pool)

		_, err := sf.Acquire()
		require.NoError(t, err)
		sf.Release()
		require.NoError(t, sf.Close())
		assert.Equal(t, 0, pool.Stats().Active,
			"terminal Close must remove the member from the pool")

		// Repeat Close is idempotent.
		require.NoError(t, sf.Close())
		assert.Equal(t, 0, pool.Stats().Active)
	})

	t.Run("ClosesWithActiveRefs", func(t *testing.T) {
		t.Parallel()
		open, _, _ := newOpener(t, []byte("activeref"))
		pool := fdpool.New(8)
		sf := NewWithPool(open, time.Hour, pool)

		_, err := sf.Acquire()
		require.NoError(t, err)
		require.NoError(t, sf.Close())
		assert.Equal(t, 0, pool.Stats().Active,
			"Forget must fire on Close even when refs are still active")
		sf.Release()
	})
}

// TestSharedFile_Pool_EvictionReleasesFD wires a tiny pool and
// confirms that the LRU eviction path drives the inline FD close
// via ReleaseNow.
func TestSharedFile_Pool_EvictionReleasesFD(t *testing.T) {
	t.Parallel()
	openA, _, handlesA := newOpener(t, []byte("a"))
	openB, _, _ := newOpener(t, []byte("b"))
	pool := fdpool.New(1)

	a := NewWithPool(openA, time.Hour, pool)
	defer a.Close()
	b := NewWithPool(openB, time.Hour, pool)
	defer b.Close()

	_, err := a.Acquire()
	require.NoError(t, err)
	a.Release()
	require.Len(t, *handlesA, 1)
	require.False(t, (*handlesA)[0].closed.Load(),
		"a's FD stays open under quiescence")

	_, err = b.Acquire()
	require.NoError(t, err)
	b.Release()

	assert.Equal(t, uint64(1), pool.Stats().Evictions,
		"b's registration should evict a")
	assert.True(t, (*handlesA)[0].closed.Load(),
		"evicted member must close its FD")
}

// TestSharedFile_NoPool_ImmediateClose locks in the pool-less
// policy: Release with refs==0 closes the FD inline (after the
// grace timer, which we make short to keep the test fast).
func TestSharedFile_NoPool_ImmediateClose(t *testing.T) {
	t.Parallel()
	open, opens, handles := newOpener(t, []byte("nopool"))
	sf := New(open, 5*time.Millisecond)
	defer sf.Close()

	_, err := sf.Acquire()
	require.NoError(t, err)
	sf.Release()

	// Wait for the grace timer to fire.
	assert.Eventually(t, func() bool {
		return len(*handles) > 0 && (*handles)[0].closed.Load()
	}, time.Second, 5*time.Millisecond,
		"pool-less SharedFile must close after grace period")

	_, err = sf.Acquire()
	require.NoError(t, err)
	assert.Equal(t, int64(2), opens.Load(),
		"second Acquire must reopen after grace-period close")
	sf.Release()
}
