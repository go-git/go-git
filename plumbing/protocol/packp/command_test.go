package packp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
)

func TestCommandRequestEncodeEmpty(t *testing.T) {
	t.Parallel()

	cmd := &CommandRequest{}
	var buf bytes.Buffer
	require.NoError(t, cmd.Encode(&buf))

	length, _, err := pktline.ReadLine(&buf)
	require.NoError(t, err)
	assert.Equal(t, pktline.Flush, length)
}

func TestCommandRequestEncodeWithArgs(t *testing.T) {
	t.Parallel()

	cmd := &CommandRequest{
		Command: "fetch",
		Capabilities: func() capability.List {
			var caps capability.List
			caps.Add(capability.Agent, "go-git/6.x")
			caps.Add(capability.ObjectFormat, "sha1")
			return caps
		}(),
		Args: &FetchArgs{
			Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
			Done:  true,
		},
	}

	var buf bytes.Buffer
	require.NoError(t, cmd.Encode(&buf))

	// Verify structure: command, caps, delim, args, flush
	l, line, err := pktline.ReadLine(&buf)
	require.NoError(t, err)
	require.True(t, l >= 4)
	assert.Equal(t, "command=fetch\n", string(line))

	l, line, err = pktline.ReadLine(&buf)
	require.NoError(t, err)
	require.True(t, l >= 4)
	assert.Equal(t, "agent=go-git/6.x\n", string(line))

	l, line, err = pktline.ReadLine(&buf)
	require.NoError(t, err)
	require.True(t, l >= 4)
	assert.Equal(t, "object-format=sha1\n", string(line))

	l, _, err = pktline.ReadLine(&buf)
	require.NoError(t, err)
	assert.Equal(t, pktline.Delim, l)

	l, line, err = pktline.ReadLine(&buf)
	require.NoError(t, err)
	require.True(t, l >= 4)
	assert.Equal(t, "want 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n", string(line))

	l, _, err = pktline.ReadLine(&buf)
	require.NoError(t, err)
	require.True(t, l >= 4)

	l, _, err = pktline.ReadLine(&buf)
	require.NoError(t, err)
	assert.Equal(t, pktline.Flush, l)
}

func TestCommandRequestDecodeEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteFlush(&buf)

	cmd := &CommandRequest{}
	require.NoError(t, cmd.Decode(&buf))
	assert.Empty(t, cmd.Command)
	assert.True(t, cmd.Capabilities.IsEmpty())
}

func TestCommandRequestDecodeWithArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "command=fetch\n")
	pktline.WriteString(&buf, "agent=go-git/6.x\n")
	pktline.WriteString(&buf, "object-format=sha1\n")
	pktline.WriteDelim(&buf)
	pktline.Writef(&buf, "want %s\n", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	pktline.WriteString(&buf, "done\n")
	pktline.WriteFlush(&buf)

	cmd := &CommandRequest{Args: &FetchArgs{}}
	require.NoError(t, cmd.Decode(&buf))
	assert.Equal(t, "fetch", cmd.Command)
	assert.True(t, cmd.Capabilities.Supports(capability.Agent))
	assert.True(t, cmd.Capabilities.Supports(capability.ObjectFormat))

	args, ok := cmd.Args.(*FetchArgs)
	require.True(t, ok)
	assert.Len(t, args.Wants, 1)
	assert.True(t, args.Done)
}

func TestCommandRequestDecodeNoArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "command=ls-refs\n")
	pktline.WriteString(&buf, "agent=go-git/6.x\n")
	pktline.WriteDelim(&buf)
	pktline.WriteFlush(&buf)

	cmd := &CommandRequest{}
	require.NoError(t, cmd.Decode(&buf))
	assert.Equal(t, "ls-refs", cmd.Command)
	assert.True(t, cmd.Capabilities.Supports(capability.Agent))
	assert.Nil(t, cmd.Args)
}

func TestCommandRequestRoundTrip(t *testing.T) {
	t.Parallel()

	cmd := &CommandRequest{
		Command: "fetch",
		Capabilities: func() capability.List {
			var caps capability.List
			caps.Add(capability.Agent, "go-git/6.x")
			return caps
		}(),
		Args: &FetchArgs{
			Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
			Done:  true,
		},
	}

	var buf bytes.Buffer
	require.NoError(t, cmd.Encode(&buf))

	got := &CommandRequest{Args: &FetchArgs{}}
	require.NoError(t, got.Decode(&buf))
	assert.Equal(t, cmd.Command, got.Command)
	assert.Equal(t, cmd.Capabilities.Get(capability.Agent), got.Capabilities.Get(capability.Agent))

	args, ok := got.Args.(*FetchArgs)
	require.True(t, ok)
	assert.Equal(t, []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")}, args.Wants)
	assert.True(t, args.Done)
}

func TestCommandRequestDecodeInvalidLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "not-a-command\n")

	cmd := &CommandRequest{}
	err := cmd.Decode(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected command line")
}
