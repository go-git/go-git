package packp

import (
	"bytes"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLsRefsArgsEncode(t *testing.T) {
	t.Parallel()

	t.Run("all arguments", func(t *testing.T) {
		t.Parallel()
		req := &LsRefsArgs{
			Peel:        true,
			Symrefs:     true,
			Unborn:      true,
			RefPrefixes: []string{"refs/heads/", "refs/tags/"},
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))

		got := &LsRefsArgs{}
		require.NoError(t, got.Decode(&buf))

		assert.True(t, got.Peel)
		assert.True(t, got.Symrefs)
		assert.True(t, got.Unborn)
		assert.Equal(t, []string{"refs/heads/", "refs/tags/"}, got.RefPrefixes)
	})

	t.Run("minimal request", func(t *testing.T) {
		t.Parallel()
		req := &LsRefsArgs{
			Symrefs: true,
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))

		got := &LsRefsArgs{}
		require.NoError(t, got.Decode(&buf))

		assert.False(t, got.Peel)
		assert.True(t, got.Symrefs)
		assert.False(t, got.Unborn)
		assert.Nil(t, got.RefPrefixes)
	})

	t.Run("round trip", func(t *testing.T) {
		t.Parallel()
		req := &LsRefsArgs{
			Peel:        true,
			Symrefs:     true,
			RefPrefixes: []string{"refs/heads/"},
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))

		got := &LsRefsArgs{}
		require.NoError(t, got.Decode(&buf))
		assert.Equal(t, req.Peel, got.Peel)
		assert.Equal(t, req.Symrefs, got.Symrefs)
		assert.Equal(t, req.Unborn, got.Unborn)
		assert.Equal(t, req.RefPrefixes, got.RefPrefixes)
	})
}

func TestLsRefsOutputDecode(t *testing.T) {
	t.Parallel()

	t.Run("simple refs", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.Writef(&buf, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/main\n")
		pktline.Writef(&buf, "a6930aaee06755d1bdcfd943fbf614e4d92bb0c7 refs/heads/develop\n")
		pktline.Writef(&buf, "5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v1.0\n")
		pktline.WriteFlush(&buf)

		resp := &LsRefsOutput{}
		require.NoError(t, resp.Decode(&buf))
		require.Len(t, resp.References, 3)

		assert.Equal(t, "refs/heads/main", resp.References[0].Name().String())
		assert.Equal(t, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), resp.References[0].Hash())
		assert.Equal(t, "refs/heads/develop", resp.References[1].Name().String())
		assert.Equal(t, "refs/tags/v1.0", resp.References[2].Name().String())
	})

	t.Run("refs with symref-target", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.Writef(&buf, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD symref-target:refs/heads/main\n")
		pktline.Writef(&buf, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/main\n")
		pktline.WriteFlush(&buf)

		resp := &LsRefsOutput{}
		require.NoError(t, resp.Decode(&buf))
		require.Len(t, resp.References, 2)

		assert.Equal(t, plumbing.SymbolicReference, resp.References[0].Type())
		assert.Equal(t, "HEAD", resp.References[0].Name().String())
		assert.Equal(t, "refs/heads/main", resp.References[0].Target().String())

		assert.Equal(t, plumbing.HashReference, resp.References[1].Type())
		assert.Equal(t, "refs/heads/main", resp.References[1].Name().String())
	})

	t.Run("refs with peeled attribute", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.Writef(&buf, "5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c refs/tags/v1.0 peeled:c39ae07f393806ccf406ef966e9a15afc43cc36a\n")
		pktline.WriteFlush(&buf)

		resp := &LsRefsOutput{}
		require.NoError(t, resp.Decode(&buf))
		require.Len(t, resp.References, 2)

		assert.Equal(t, "refs/tags/v1.0", resp.References[0].Name().String())
		assert.Equal(t, plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"), resp.References[0].Hash())

		assert.Equal(t, "refs/tags/v1.0^{}", resp.References[1].Name().String())
		assert.Equal(t, plumbing.NewHash("c39ae07f393806ccf406ef966e9a15afc43cc36a"), resp.References[1].Hash())
	})

	t.Run("unborn ref with symref-target", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.Writef(&buf, "unborn HEAD symref-target:refs/heads/main\n")
		pktline.Writef(&buf, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/main\n")
		pktline.WriteFlush(&buf)

		resp := &LsRefsOutput{}
		require.NoError(t, resp.Decode(&buf))
		require.Len(t, resp.References, 2)

		assert.Equal(t, plumbing.SymbolicReference, resp.References[0].Type())
		assert.Equal(t, "HEAD", resp.References[0].Name().String())
		assert.Equal(t, "refs/heads/main", resp.References[0].Target().String())

		assert.Equal(t, plumbing.HashReference, resp.References[1].Type())
		assert.Equal(t, "refs/heads/main", resp.References[1].Name().String())
	})
}

func TestLsRefsOutputEncode(t *testing.T) {
	t.Parallel()

	t.Run("simple refs", func(t *testing.T) {
		t.Parallel()
		resp := &LsRefsOutput{
			References: []*plumbing.Reference{
				plumbing.NewHashReference("refs/heads/main", plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")),
				plumbing.NewHashReference("refs/heads/develop", plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7")),
			},
		}

		var buf bytes.Buffer
		require.NoError(t, resp.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &LsRefsOutput{}
		require.NoError(t, got.Decode(&buf))
		require.Len(t, got.References, 2)
		assert.Equal(t, "refs/heads/main", got.References[0].Name().String())
		assert.Equal(t, "refs/heads/develop", got.References[1].Name().String())
	})

	t.Run("symbolic reference", func(t *testing.T) {
		t.Parallel()
		resp := &LsRefsOutput{
			References: []*plumbing.Reference{
				plumbing.NewSymbolicReference("HEAD", "refs/heads/main"),
				plumbing.NewHashReference("refs/heads/main", plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")),
			},
		}

		var buf bytes.Buffer
		require.NoError(t, resp.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &LsRefsOutput{}
		require.NoError(t, got.Decode(&buf))
		require.Len(t, got.References, 2)

		assert.Equal(t, plumbing.SymbolicReference, got.References[0].Type())
		assert.Equal(t, "HEAD", got.References[0].Name().String())
		assert.Equal(t, "refs/heads/main", got.References[0].Target().String())

		assert.Equal(t, plumbing.HashReference, got.References[1].Type())
		assert.Equal(t, "refs/heads/main", got.References[1].Name().String())
	})

	t.Run("round trip with peeled refs", func(t *testing.T) {
		t.Parallel()
		resp := &LsRefsOutput{
			References: []*plumbing.Reference{
				plumbing.NewHashReference("refs/tags/v1.0", plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c")),
				plumbing.NewHashReference("refs/tags/v1.0^{}", plumbing.NewHash("c39ae07f393806ccf406ef966e9a15afc43cc36a")),
			},
		}

		var buf bytes.Buffer
		require.NoError(t, resp.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &LsRefsOutput{}
		require.NoError(t, got.Decode(&buf))
		require.Len(t, got.References, 2)
		assert.Equal(t, "refs/tags/v1.0", got.References[0].Name().String())
		assert.Equal(t, "refs/tags/v1.0^{}", got.References[1].Name().String())
		assert.Equal(t, plumbing.NewHash("c39ae07f393806ccf406ef966e9a15afc43cc36a"), got.References[1].Hash())
	})
}

