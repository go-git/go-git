package packp

import (
	"bytes"
	"errors"
	"io"
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

func TestCommandRequestDecodeNoCommand(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "not-a-command\n")
	pktline.WriteDelim(&buf)
	pktline.WriteFlush(&buf)

	cmd := &CommandRequest{}
	err := cmd.Decode(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command requested")
}

func TestCommandRequestDecodeTruncated(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "command=fetch\n")

	cmd := &CommandRequest{}
	require.ErrorIs(t, cmd.Decode(&buf), io.ErrUnexpectedEOF)
}

// Upstream serve.c (process_request) accepts the command line anywhere among
// the capability lines, and git's own client sends its capabilities first for
// "command=bundle-uri" (connect.c get_remote_bundle_uri).
func TestCommandRequestDecodeCapabilitiesBeforeCommand(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "agent=git/2.55.0\n")
	pktline.WriteString(&buf, "object-format=sha1\n")
	pktline.WriteString(&buf, "command=fetch\n")
	pktline.WriteString(&buf, "session-id=abc\n")
	pktline.WriteDelim(&buf)
	pktline.Writef(&buf, "want %s\n", "6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	pktline.WriteString(&buf, "done\n")
	pktline.WriteFlush(&buf)

	cmd := &CommandRequest{Args: &FetchArgs{}}
	require.NoError(t, cmd.Decode(&buf))
	assert.Equal(t, "fetch", cmd.Command)
	assert.Equal(t, []string{"git/2.55.0"}, cmd.Capabilities.Get(capability.Agent))
	assert.Equal(t, []string{"sha1"}, cmd.Capabilities.Get(capability.ObjectFormat))
	assert.Equal(t, []string{"abc"}, cmd.Capabilities.Get(capability.SessionID))
	assert.False(t, cmd.Capabilities.Supports("command"))

	args, ok := cmd.Args.(*FetchArgs)
	require.True(t, ok)
	assert.Len(t, args.Wants, 1)
	assert.True(t, args.Done)
}

func TestCommandRequestDecodeDuplicateCommand(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "command=ls-refs\n")
	pktline.WriteString(&buf, "command=fetch\n")
	pktline.WriteDelim(&buf)
	pktline.WriteFlush(&buf)

	cmd := &CommandRequest{}
	err := cmd.Decode(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already requesting command")
}

// An empty command is still a command: one requested after it is a duplicate.
func TestCommandRequestDecodeEmptyThenCommand(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "command=\n")
	pktline.WriteString(&buf, "command=ls-refs\n")
	pktline.WriteDelim(&buf)
	pktline.WriteFlush(&buf)

	cmd := &CommandRequest{}
	err := cmd.Decode(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already requesting command")
}

// Without command arguments the delim-pkt may be omitted, as in upstream
// serve.c where the flush-pkt then also ends the arguments.
func TestCommandRequestDecodeWithoutDelim(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteString(&buf, "object-format=sha1\n")
	pktline.WriteString(&buf, "command=ls-refs\n")
	pktline.WriteFlush(&buf)
	pktline.WriteString(&buf, "next\n")

	cmd := &CommandRequest{NewArgs: func(string) (CommandArgs, error) { return &LsRefsArgs{}, nil }}
	require.NoError(t, cmd.Decode(&buf))
	assert.Equal(t, "ls-refs", cmd.Command)
	assert.Equal(t, &LsRefsArgs{}, cmd.Args)

	// The flush-pkt ending the request is consumed, and nothing after it.
	_, line, err := pktline.ReadLine(&buf)
	require.NoError(t, err)
	assert.Equal(t, "next\n", string(line))
}

func TestCommandRequestDecodeNewArgs(t *testing.T) {
	t.Parallel()

	newArgs := func(command string) (CommandArgs, error) {
		switch command {
		case "ls-refs":
			return &LsRefsArgs{}, nil
		case "fetch":
			return &FetchArgs{}, nil
		}
		return nil, errors.New("unknown command")
	}

	var buf bytes.Buffer
	pktline.WriteString(&buf, "object-format=sha1\n")
	pktline.WriteString(&buf, "command=ls-refs\n")
	pktline.WriteDelim(&buf)
	pktline.WriteString(&buf, "ref-prefix refs/heads/\n")
	pktline.WriteFlush(&buf)

	cmd := &CommandRequest{NewArgs: newArgs}
	require.NoError(t, cmd.Decode(&buf))
	args, ok := cmd.Args.(*LsRefsArgs)
	require.True(t, ok)
	assert.Equal(t, []string{"refs/heads/"}, args.RefPrefixes)

	buf.Reset()
	pktline.WriteString(&buf, "object-format=sha1\n")
	pktline.WriteString(&buf, "command=bogus\n")
	pktline.WriteDelim(&buf)
	pktline.WriteFlush(&buf)

	cmd = &CommandRequest{NewArgs: newArgs}
	require.EqualError(t, cmd.Decode(&buf), "unknown command")
	assert.Equal(t, "bogus", cmd.Command)
}
