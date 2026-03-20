package capability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/protocol"
)

func TestNewCapabilitiesV1(t *testing.T) {
	t.Parallel()

	list := NewList()
	require.NoError(t, list.Set(OFSDelta))
	require.NoError(t, list.Set(Agent, "go-git/test"))
	require.NoError(t, list.Set(Sideband64k))

	caps := NewCapabilitiesV1(list)

	assert.Equal(t, protocol.V0, caps.Version())
	assert.True(t, caps.Supports(OFSDelta))
	assert.True(t, caps.Supports(Agent))
	assert.True(t, caps.Supports(Sideband64k))
	assert.False(t, caps.Supports(Shallow))
	assert.Equal(t, []string{"go-git/test"}, caps.Get(Agent))
	assert.Nil(t, caps.Get(Shallow))
}

func TestNewCapabilitiesV1Nil(t *testing.T) {
	t.Parallel()

	caps := NewCapabilitiesV1(nil)
	assert.Equal(t, protocol.V0, caps.Version())
	assert.False(t, caps.Supports(OFSDelta))
	assert.Nil(t, caps.Get(Agent))
}

func TestCapabilitiesV1List(t *testing.T) {
	t.Parallel()

	list := NewList()
	require.NoError(t, list.Set(OFSDelta))

	caps := NewCapabilitiesV1(list)
	assert.Same(t, list, caps.V1List())
}

func TestCapabilitiesV1SupportsCommand(t *testing.T) {
	t.Parallel()

	caps := NewCapabilitiesV1(NewList())
	// V0/V1 always returns true for SupportsCommand
	assert.True(t, caps.SupportsCommand("fetch"))
	assert.True(t, caps.SupportsCommand("ls-refs"))
	assert.True(t, caps.SupportsCommand("anything"))
}

func TestCapabilitiesV1CommandCapabilities(t *testing.T) {
	t.Parallel()

	list := NewList()
	require.NoError(t, list.Set(OFSDelta))

	caps := NewCapabilitiesV1(list)
	// V0/V1 returns the flat list for any command
	assert.Same(t, list, caps.CommandCapabilities("fetch"))
	assert.Same(t, list, caps.CommandCapabilities("ls-refs"))
}

func TestNewCapabilitiesV2(t *testing.T) {
	t.Parallel()

	global := NewList()
	require.NoError(t, global.Set(Agent, "git/2.45.0"))
	require.NoError(t, global.Set(ObjectFormat, "sha1"))

	fetchCaps := NewList()
	require.NoError(t, fetchCaps.Add(Shallow))
	require.NoError(t, fetchCaps.Add(Filter))
	require.NoError(t, fetchCaps.Add(OFSDelta))

	lsRefsCaps := NewList()
	require.NoError(t, lsRefsCaps.Add(SymRef, "HEAD:refs/heads/main"))

	commands := map[string]*List{
		"fetch":   fetchCaps,
		"ls-refs": lsRefsCaps,
	}

	caps := NewCapabilitiesV2(global, commands)

	assert.Equal(t, protocol.V2, caps.Version())

	// Global capabilities
	assert.True(t, caps.Supports(Agent))
	assert.Equal(t, []string{"git/2.45.0"}, caps.Get(Agent))
	assert.True(t, caps.Supports(ObjectFormat))

	// Fetch sub-capabilities
	assert.True(t, caps.Supports(Shallow))
	assert.True(t, caps.Supports(Filter))
	assert.True(t, caps.Supports(OFSDelta))

	// ls-refs sub-capabilities
	assert.True(t, caps.Supports(SymRef))

	// Non-existent capability
	assert.False(t, caps.Supports(MultiACK))
	assert.Nil(t, caps.Get(MultiACK))
}

func TestCapabilitiesV2SupportsCommand(t *testing.T) {
	t.Parallel()

	commands := map[string]*List{
		"fetch":   NewList(),
		"ls-refs": NewList(),
	}
	caps := NewCapabilitiesV2(nil, commands)

	assert.True(t, caps.SupportsCommand("fetch"))
	assert.True(t, caps.SupportsCommand("ls-refs"))
	assert.False(t, caps.SupportsCommand("object-info"))
}

func TestCapabilitiesV2CommandCapabilities(t *testing.T) {
	t.Parallel()

	fetchCaps := NewList()
	require.NoError(t, fetchCaps.Add(Shallow))

	commands := map[string]*List{
		"fetch": fetchCaps,
	}
	caps := NewCapabilitiesV2(nil, commands)

	assert.Same(t, fetchCaps, caps.CommandCapabilities("fetch"))
	// Non-existent command returns empty list
	unknown := caps.CommandCapabilities("object-info")
	assert.NotNil(t, unknown)
	assert.True(t, unknown.IsEmpty())
}

func TestCapabilitiesV2V1ListReturnsNil(t *testing.T) {
	t.Parallel()

	caps := NewCapabilitiesV2(nil, nil)
	assert.Nil(t, caps.V1List())
}

func TestCapabilitiesV2NilArgs(t *testing.T) {
	t.Parallel()

	caps := NewCapabilitiesV2(nil, nil)
	assert.Equal(t, protocol.V2, caps.Version())
	assert.False(t, caps.Supports(OFSDelta))
	assert.False(t, caps.SupportsCommand("fetch"))
}
