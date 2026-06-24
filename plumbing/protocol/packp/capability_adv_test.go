package packp

import (
	"bytes"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/stretchr/testify/assert"
)

func TestCapabilityAdvDecode(t *testing.T) {
	t.Parallel()

	t.Run("basic capabilities", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "version 2\n")
		pktline.WriteString(&buf, "agent=git/2.45.0\n")
		pktline.WriteString(&buf, "ls-refs=unborn\n")
		pktline.WriteString(&buf, "fetch=shallow wait-for-done filter\n")
		pktline.WriteString(&buf, "server-option\n")
		pktline.WriteString(&buf, "object-format=sha1\n")
		pktline.WriteFlush(&buf)

		ca := &CapabilityAdv{}
		assert.NoError(t, ca.Decode(&buf))
		assert.Equal(t, protocol.V2, ca.Version)
		assert.True(t, ca.Capabilities.Supports(capability.Agent))
		assert.Equal(t, []string{"git/2.45.0"}, ca.Capabilities.Get(capability.Agent))
		assert.True(t, ca.Capabilities.Supports(capability.LsRefs))
		assert.Equal(t, []string{"unborn"}, ca.Capabilities.Get(capability.LsRefs))
		assert.True(t, ca.Capabilities.Supports(capability.FetchCmd))
		assert.Equal(t, []string{"shallow", "wait-for-done", "filter"}, ca.Capabilities.Get(capability.FetchCmd))
		assert.True(t, ca.Capabilities.Supports(capability.ServerOption))
		assert.True(t, ca.Capabilities.Supports(capability.ObjectFormat))
		assert.Equal(t, []string{"sha1"}, ca.Capabilities.Get(capability.ObjectFormat))
	})

	t.Run("capability without value", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "version 2\n")
		pktline.WriteString(&buf, "agent=git/2.0.0\n")
		pktline.WriteString(&buf, "ls-refs\n")
		pktline.WriteFlush(&buf)

		ca := &CapabilityAdv{}
		assert.NoError(t, ca.Decode(&buf))
		assert.True(t, ca.Capabilities.Supports(capability.Agent))
		assert.True(t, ca.Capabilities.Supports(capability.LsRefs))
		assert.Nil(t, ca.Capabilities.Get(capability.LsRefs))
	})

	t.Run("empty advertisement", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "version 2\n")
		pktline.WriteFlush(&buf)

		ca := &CapabilityAdv{}
		assert.NoError(t, ca.Decode(&buf))
		assert.Equal(t, protocol.V2, ca.Version)
		assert.True(t, ca.Capabilities.IsEmpty())
	})

	t.Run("preserves existing capabilities", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "version 2\n")
		pktline.WriteString(&buf, "ls-refs\n")
		pktline.WriteFlush(&buf)

		caps := capability.List{}
		caps.Add(capability.Agent, "test/1.0")
		ca := &CapabilityAdv{Capabilities: caps}
		assert.NoError(t, ca.Decode(&buf))
		assert.Equal(t, []string{"test/1.0"}, ca.Capabilities.Get(capability.Agent))
		assert.True(t, ca.Capabilities.Supports(capability.LsRefs))
	})
}

func TestCapabilityAdvEncode(t *testing.T) {
	t.Parallel()

	t.Run("basic capabilities", func(t *testing.T) {
		t.Parallel()
		caps := capability.List{}
		caps.Add(capability.Agent, "git/2.45.0")
		caps.Add(capability.LsRefs, "unborn")
		caps.Add(capability.FetchCmd, "shallow", "wait-for-done", "filter")
		caps.Add(capability.ServerOption)
		caps.Add(capability.ObjectFormat, "sha1")

		ca := &CapabilityAdv{
			Version:      protocol.V2,
			Capabilities: caps,
		}

		var buf bytes.Buffer
		assert.NoError(t, ca.Encode(&buf))

		got := &CapabilityAdv{}
		assert.NoError(t, got.Decode(&buf))
		assert.Equal(t, protocol.V2, got.Version)
		assert.Equal(t, []string{"git/2.45.0"}, got.Capabilities.Get(capability.Agent))
		assert.Equal(t, []string{"unborn"}, got.Capabilities.Get(capability.LsRefs))
		assert.Equal(t, []string{"shallow", "wait-for-done", "filter"}, got.Capabilities.Get(capability.FetchCmd))
		assert.True(t, got.Capabilities.Supports(capability.ServerOption))
		assert.Equal(t, []string{"sha1"}, got.Capabilities.Get(capability.ObjectFormat))
	})

	t.Run("empty capabilities", func(t *testing.T) {
		t.Parallel()
		ca := &CapabilityAdv{
			Version:      protocol.V2,
			Capabilities: capability.List{},
		}

		var buf bytes.Buffer
		assert.NoError(t, ca.Encode(&buf))

		got := &CapabilityAdv{}
		assert.NoError(t, got.Decode(&buf))
		assert.Equal(t, protocol.V2, got.Version)
		assert.True(t, got.Capabilities.IsEmpty())
	})

	t.Run("encode produces version line first", func(t *testing.T) {
		t.Parallel()
		caps := capability.List{}
		caps.Add(capability.Agent, "go-git/6.x")
		ca := &CapabilityAdv{
			Version:      protocol.V2,
			Capabilities: caps,
		}

		var buf bytes.Buffer
		assert.NoError(t, ca.Encode(&buf))

		l, line, err := pktline.ReadLine(&buf)
		assert.NoError(t, err)
		assert.True(t, l >= 4)
		assert.Equal(t, "version 2\n", string(line))
	})

	t.Run("capability without values", func(t *testing.T) {
		t.Parallel()
		caps := capability.List{}
		caps.Add(capability.LsRefs)
		caps.Add(capability.ServerOption)
		ca := &CapabilityAdv{
			Version:      protocol.V2,
			Capabilities: caps,
		}

		var buf bytes.Buffer
		assert.NoError(t, ca.Encode(&buf))

		got := &CapabilityAdv{}
		assert.NoError(t, got.Decode(&buf))
		assert.True(t, got.Capabilities.Supports(capability.LsRefs))
		assert.Nil(t, got.Capabilities.Get(capability.LsRefs))
		assert.True(t, got.Capabilities.Supports(capability.ServerOption))
	})
}
