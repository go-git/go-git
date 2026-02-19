package plugin

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetGlobal() {
	mu.Lock()
	defer mu.Unlock()
	entries = map[Name]*entry{}
}

func TestRegisterAndGet(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	key := newKey[string]("greeting")

	require.NoError(t, Register(key, func() string { return "hello" }))

	got, err := Get(key)
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestGetNotFound(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	key := newKey[int]("empty")

	got, err := Get(key)
	require.Zero(t, got)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRegisterNilFactory(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	key := newKey[int]("nil-factory")

	err := Register(key, nil)
	assert.ErrorIs(t, err, ErrNilFactory)
}

func TestRegisterOverwrite(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	key := newKey[string]("overwrite")

	Register(key, func() string { return "first" })
	Register(key, func() string { return "second" })

	got, err := Get(key)
	require.NoError(t, err)
	assert.Equal(t, "second", got)
}

func TestAutoFreeze(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	key := newKey[int]("freeze")

	Register(key, func() int { return 1 })

	got, err := Get(key)
	require.NoError(t, err)
	assert.Equal(t, 1, got)

	err = Register(key, func() int { return 2 })
	assert.ErrorIs(t, err, ErrFrozen)

	got, err = Get(key)
	require.NoError(t, err)
	assert.Equal(t, 1, got)
}

func TestGetNotFoundAlsoFreezes(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	key := newKey[int]("freeze-on-miss")

	got, err := Get(key)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Zero(t, got)

	err = Register(key, func() int { return 1 })
	assert.ErrorIs(t, err, ErrFrozen)
}

func TestHas(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	key := newKey[int]("has-test")

	assert.False(t, Has(key))

	Register(key, func() int { return 1 })

	assert.True(t, Has(key))
}

func TestGetReturnsNewInstance(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	counter := 0
	key := newKey[int]("counter")
	Register(key, func() int {
		counter++
		return counter
	})

	a, err := Get(key)
	require.NoError(t, err)

	b, err := Get(key)
	require.NoError(t, err)

	assert.NotEqual(t, a, b, "factory should be called each time")
}

func TestConcurrentAccess(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	key := newKey[int]("concurrent")
	Register(key, func() int { return 42 })

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			Get(key)
			Has(key)
		}(i)
	}
	wg.Wait()
}

func TestMultipleKeysSameType(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	type Signer struct{ name string }

	gpgSigner := newKey[Signer]("gpg-signer")
	sshSigner := newKey[Signer]("ssh-signer")

	Register(gpgSigner, func() Signer { return Signer{name: "gpg"} })
	Register(sshSigner, func() Signer { return Signer{name: "ssh"} })

	gs, _ := Get(gpgSigner)
	ss, _ := Get(sshSigner)
	assert.Equal(t, "gpg", gs.name)
	assert.Equal(t, "ssh", ss.name)
}

func TestPerKeyFreezeIsolation(t *testing.T) { //nolint:paralleltest // modifies global trace target
	resetGlobal()
	a := newKey[string]("a")
	b := newKey[string]("b")

	Register(a, func() string { return "ax" })
	Register(b, func() string { return "bx" })

	Get(a)

	assert.ErrorIs(t, Register(a, func() string { return "ay" }), ErrFrozen)
	assert.NoError(t, Register(b, func() string { return "by" }))
}
