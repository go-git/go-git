package packp

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func encodeLsRefsLines(lines ...string) []byte {
	var buf bytes.Buffer
	for _, l := range lines {
		pktline.Writeln(&buf, l)
	}
	pktline.WriteFlush(&buf)
	return buf.Bytes()
}

func TestLsRefsRequestEncode(t *testing.T) {
	t.Parallel()

	req := &LsRefsRequest{
		RefPrefixes:    []string{"refs/heads/", "refs/tags/"},
		IncludeSymRefs: true,
		IncludePeeled:  true,
		IncludeUnborn:  false,
	}

	var buf bytes.Buffer
	err := req.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "command=ls-refs")
	assert.Contains(t, data, "agent=")
	assert.Contains(t, data, "peel")
	assert.Contains(t, data, "symrefs")
	assert.NotContains(t, data, "unborn")
	assert.Contains(t, data, "ref-prefix refs/heads/")
	assert.Contains(t, data, "ref-prefix refs/tags/")
}

func TestLsRefsRequestEncodeMinimal(t *testing.T) {
	t.Parallel()

	req := &LsRefsRequest{}

	var buf bytes.Buffer
	err := req.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "command=ls-refs")
	assert.NotContains(t, data, "peel")
	assert.NotContains(t, data, "symrefs")
	assert.NotContains(t, data, "unborn")
	assert.NotContains(t, data, "ref-prefix")
}

func TestLsRefsResponseDecode(t *testing.T) {
	t.Parallel()

	hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	peeledHash := "1111111111111111111111111111111111111111"

	input := encodeLsRefsLines(
		hash+" refs/heads/master",
		hash+" HEAD symref-target:refs/heads/master",
		hash+" refs/tags/v1.0 peeled:"+peeledHash,
	)

	resp := NewLsRefsResponse()
	err := resp.Decode(bytes.NewReader(input))
	require.NoError(t, err)

	require.Len(t, resp.References, 3)

	// Regular ref
	assert.Equal(t, plumbing.ReferenceName("refs/heads/master"), resp.References[0].Name())
	assert.Equal(t, plumbing.NewHash(hash), resp.References[0].Hash())
	assert.Equal(t, plumbing.HashReference, resp.References[0].Type())

	// Symref
	assert.Equal(t, plumbing.ReferenceName("HEAD"), resp.References[1].Name())
	assert.Equal(t, plumbing.ReferenceName("refs/heads/master"), resp.References[1].Target())
	assert.Equal(t, plumbing.SymbolicReference, resp.References[1].Type())

	// Peeled
	assert.Equal(t, plumbing.ReferenceName("refs/tags/v1.0"), resp.References[2].Name())
	peeledVal, ok := resp.Peeled["refs/tags/v1.0"]
	assert.True(t, ok)
	assert.Equal(t, plumbing.NewHash(peeledHash), peeledVal)
}

func TestLsRefsResponseDecodeEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pktline.WriteFlush(&buf)

	resp := NewLsRefsResponse()
	err := resp.Decode(&buf)
	require.NoError(t, err)
	assert.Len(t, resp.References, 0)
}

func TestLsRefsResponseEncodeDecode(t *testing.T) {
	t.Parallel()

	hash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	peeledHash := plumbing.NewHash("1111111111111111111111111111111111111111")

	original := NewLsRefsResponse()
	original.References = []*plumbing.Reference{
		plumbing.NewHashReference("refs/heads/master", hash),
		plumbing.NewHashReference("refs/tags/v1.0", hash),
	}
	original.Peeled["refs/tags/v1.0"] = peeledHash

	var buf bytes.Buffer
	err := original.Encode(&buf)
	require.NoError(t, err)

	decoded := NewLsRefsResponse()
	err = decoded.Decode(&buf)
	require.NoError(t, err)

	require.Len(t, decoded.References, 2)
	assert.Equal(t, plumbing.ReferenceName("refs/heads/master"), decoded.References[0].Name())
	assert.Equal(t, hash, decoded.References[0].Hash())

	assert.Equal(t, plumbing.ReferenceName("refs/tags/v1.0"), decoded.References[1].Name())
	peeledVal, ok := decoded.Peeled["refs/tags/v1.0"]
	assert.True(t, ok)
	assert.Equal(t, peeledHash, peeledVal)
}
