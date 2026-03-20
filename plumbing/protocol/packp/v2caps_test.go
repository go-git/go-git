package packp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"
)

func encodeCapLines(lines ...string) []byte {
	var buf bytes.Buffer
	for _, l := range lines {
		pktline.Writeln(&buf, l)
	}
	pktline.WriteFlush(&buf)
	return buf.Bytes()
}

func TestV2ServerCapabilitiesDecode(t *testing.T) {
	t.Parallel()

	input := encodeCapLines(
		"agent=git/2.45.0",
		"object-format=sha1",
		"server-option",
		"fetch=shallow filter ofs-delta",
		"ls-refs=unborn peel",
	)

	caps := NewV2ServerCapabilities()
	err := caps.Decode(bytes.NewReader(input))
	require.NoError(t, err)

	// Global capabilities
	assert.True(t, caps.Global.Supports(capability.Agent))
	assert.Equal(t, []string{"git/2.45.0"}, caps.Global.Get(capability.Agent))
	assert.True(t, caps.Global.Supports(capability.ObjectFormat))
	assert.True(t, caps.Global.Supports("server-option"))

	// Commands
	require.Contains(t, caps.Commands, "fetch")
	assert.True(t, caps.Commands["fetch"].Supports("shallow"))
	assert.True(t, caps.Commands["fetch"].Supports("filter"))
	assert.True(t, caps.Commands["fetch"].Supports("ofs-delta"))
	assert.False(t, caps.Commands["fetch"].Supports("thin-pack"))

	require.Contains(t, caps.Commands, "ls-refs")
	assert.True(t, caps.Commands["ls-refs"].Supports("unborn"))
	assert.True(t, caps.Commands["ls-refs"].Supports("peel"))
}

func TestV2ServerCapabilitiesDecodeNoSubcaps(t *testing.T) {
	t.Parallel()

	input := encodeCapLines(
		"agent=git/2.45.0",
		"fetch",
		"ls-refs",
	)

	caps := NewV2ServerCapabilities()
	err := caps.Decode(bytes.NewReader(input))
	require.NoError(t, err)

	require.Contains(t, caps.Commands, "fetch")
	assert.Equal(t, 0, len(caps.Commands["fetch"].All()))

	require.Contains(t, caps.Commands, "ls-refs")
	assert.Equal(t, 0, len(caps.Commands["ls-refs"].All()))
}

func TestV2ServerCapabilitiesEncodeDecode(t *testing.T) {
	t.Parallel()

	caps := NewV2ServerCapabilities()
	require.NoError(t, caps.Global.Add(capability.Agent, "go-git/test"))

	fetchCaps := capability.NewList()
	require.NoError(t, fetchCaps.Add("shallow"))
	require.NoError(t, fetchCaps.Add("filter"))
	caps.Commands["fetch"] = fetchCaps

	lsRefsCaps := capability.NewList()
	caps.Commands["ls-refs"] = lsRefsCaps

	var buf bytes.Buffer
	err := caps.Encode(&buf)
	require.NoError(t, err)

	decoded := NewV2ServerCapabilities()
	err = decoded.Decode(&buf)
	require.NoError(t, err)

	assert.True(t, decoded.Global.Supports(capability.Agent))
	assert.Equal(t, []string{"go-git/test"}, decoded.Global.Get(capability.Agent))
	require.Contains(t, decoded.Commands, "fetch")
	assert.True(t, decoded.Commands["fetch"].Supports("shallow"))
	assert.True(t, decoded.Commands["fetch"].Supports("filter"))
	require.Contains(t, decoded.Commands, "ls-refs")
}

func TestV2ServerCapabilitiesToCapabilities(t *testing.T) {
	t.Parallel()

	caps := NewV2ServerCapabilities()
	require.NoError(t, caps.Global.Add(capability.Agent, "git/2.45.0"))

	fetchCaps := capability.NewList()
	require.NoError(t, fetchCaps.Add("shallow"))
	caps.Commands["fetch"] = fetchCaps

	c := caps.ToCapabilities()
	assert.True(t, c.Supports(capability.Agent))
	assert.True(t, c.Supports("shallow"))
	assert.True(t, c.SupportsCommand("fetch"))
}
