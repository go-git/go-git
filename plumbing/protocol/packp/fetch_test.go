package packp

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func TestFetchArgsEncode(t *testing.T) {
	t.Parallel()

	t.Run("empty wants", func(t *testing.T) {
		t.Parallel()
		req := &FetchArgs{}
		var buf bytes.Buffer
		require.Error(t, req.Encode(&buf))
	})

	t.Run("wants only", func(t *testing.T) {
		t.Parallel()
		req := &FetchArgs{
			Wants: []plumbing.Hash{
				plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
			},
			Done: true,
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &FetchArgs{}
		require.NoError(t, got.Decode(&buf))
		assert.Equal(t, req.Wants, got.Wants)
		assert.True(t, got.Done)
	})

	t.Run("wants and haves", func(t *testing.T) {
		t.Parallel()
		req := &FetchArgs{
			Wants: []plumbing.Hash{
				plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
			},
			Haves: []plumbing.Hash{
				plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
			},
			Done: true,
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &FetchArgs{}
		require.NoError(t, got.Decode(&buf))
		assert.Equal(t, req.Wants, got.Wants)
		assert.Equal(t, req.Haves, got.Haves)
		assert.True(t, got.Done)
	})

	t.Run("all arguments", func(t *testing.T) {
		t.Parallel()
		req := &FetchArgs{
			Wants: []plumbing.Hash{
				plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
				plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
			},
			Haves: []plumbing.Hash{
				plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
			},
			Done:        true,
			ThinPack:    true,
			NoProgress:  true,
			IncludeTag:  true,
			OFSDelta:    true,
			Shallows:    []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
			Deepen:      1,
			DeepenSince: time.Unix(1700000000, 0).UTC(),
			DeepenNot:   []string{"refs/heads/main"},
			Filter:      FilterBlobNone(),
			WaitForDone: true,
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &FetchArgs{}
		require.NoError(t, got.Decode(&buf))
		assert.Equal(t, req.Wants, got.Wants)
		assert.Equal(t, req.Haves, got.Haves)
		assert.True(t, got.Done)
		assert.True(t, got.ThinPack)
		assert.True(t, got.NoProgress)
		assert.True(t, got.IncludeTag)
		assert.True(t, got.OFSDelta)
		assert.Equal(t, req.Shallows, got.Shallows)
		assert.Equal(t, 1, got.Deepen)
		assert.True(t, got.DeepenSince.Equal(req.DeepenSince))
		assert.Equal(t, req.DeepenNot, got.DeepenNot)
		assert.Equal(t, req.Filter, got.Filter)
		assert.True(t, got.WaitForDone)
	})

	t.Run("deepen-relative", func(t *testing.T) {
		t.Parallel()
		req := &FetchArgs{
			Wants:          []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
			Done:           true,
			Deepen:         3,
			DeepenRelative: true,
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))
		wire := buf.String()
		assert.Contains(t, wire, "deepen-relative\n", "deepen-relative must be a flag, not carry a numeric argument")
		assert.NotContains(t, wire, "deepen-relative 3")
		pktline.WriteFlush(&buf)

		got := &FetchArgs{}
		require.NoError(t, got.Decode(&buf))
		assert.True(t, got.DeepenRelative)
		assert.Equal(t, 3, got.Deepen)
	})
}

func TestFetchArgsDecodeFromWire(t *testing.T) {
	t.Parallel()

	t.Run("wants and done", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.Writef(&buf, "want 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
		pktline.WriteString(&buf, "done\n")
		pktline.WriteFlush(&buf)

		got := &FetchArgs{}
		require.NoError(t, got.Decode(&buf))
		assert.Equal(t, []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")}, got.Wants)
		assert.True(t, got.Done)
	})

	t.Run("wants haves and flags", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.Writef(&buf, "want 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
		pktline.Writef(&buf, "have a6930aaee06755d1bdcfd943fbf614e4d92bb0c7\n")
		pktline.WriteString(&buf, "done\n")
		pktline.WriteString(&buf, "thin-pack\n")
		pktline.WriteString(&buf, "ofs-delta\n")
		pktline.Writef(&buf, "filter blob:none\n")
		pktline.WriteFlush(&buf)

		got := &FetchArgs{}
		require.NoError(t, got.Decode(&buf))
		require.Len(t, got.Wants, 1)
		require.Len(t, got.Haves, 1)
		assert.True(t, got.Done)
		assert.True(t, got.ThinPack)
		assert.True(t, got.OFSDelta)
		assert.Equal(t, Filter("blob:none"), got.Filter)
	})
}

func TestFetchOutputDecode(t *testing.T) {
	t.Parallel()

	t.Run("acknowledgments only", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "acknowledgments\n")
		pktline.Writef(&buf, "ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
		pktline.WriteString(&buf, "ready\n")
		pktline.WriteDelim(&buf)
		pktline.WriteString(&buf, "packfile\n")
		// Packfile data would follow; we write a flush to simulate end.
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.NotNil(t, got.Acknowledgments)
		require.Len(t, got.Acknowledgments.ACKs, 1)
		assert.Equal(t, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), got.Acknowledgments.ACKs[0])
		assert.True(t, got.Acknowledgments.Ready)
		assert.True(t, got.Packfile)
	})

	t.Run("negotiation round has no packfile", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "acknowledgments\n")
		pktline.Writef(&buf, "ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.NotNil(t, got.Acknowledgments)
		require.Len(t, got.Acknowledgments.ACKs, 1)
		assert.False(t, got.Acknowledgments.Ready)
		assert.False(t, got.Packfile)
	})

	t.Run("done response without acknowledgments", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "packfile\n")
		pktline.WriteString(&buf, "PACK")

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		assert.Nil(t, got.Acknowledgments)
		assert.True(t, got.Packfile)
	})

	t.Run("NAK only", func(t *testing.T) {
		t.Parallel()
		// A NAK (no common objects) acknowledgments section is a negotiation
		// round: it is not ready and ends the response with a flush-pkt, so no
		// packfile follows and the client negotiates again.
		var buf bytes.Buffer
		pktline.WriteString(&buf, "acknowledgments\n")
		pktline.WriteString(&buf, "NAK\n")
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.NotNil(t, got.Acknowledgments)
		assert.Empty(t, got.Acknowledgments.ACKs)
		assert.False(t, got.Acknowledgments.Ready)
		assert.False(t, got.Packfile)
	})

	t.Run("full response with shallow-info", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "acknowledgments\n")
		pktline.Writef(&buf, "ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
		pktline.WriteString(&buf, "ready\n")
		pktline.WriteDelim(&buf)
		pktline.WriteString(&buf, "shallow-info\n")
		pktline.Writef(&buf, "shallow 1111111111111111111111111111111111111111\n")
		pktline.Writef(&buf, "unshallow 2222222222222222222222222222222222222222\n")
		pktline.WriteDelim(&buf)
		pktline.WriteString(&buf, "packfile\n")
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.NotNil(t, got.Acknowledgments)
		require.Len(t, got.Acknowledgments.ACKs, 1)
		assert.True(t, got.Acknowledgments.Ready)

		require.NotNil(t, got.ShallowInfo)
		require.Len(t, got.ShallowInfo.Shallows, 1)
		assert.Equal(t, plumbing.NewHash("1111111111111111111111111111111111111111"), got.ShallowInfo.Shallows[0])
		require.Len(t, got.ShallowInfo.Unshallows, 1)
		assert.Equal(t, plumbing.NewHash("2222222222222222222222222222222222222222"), got.ShallowInfo.Unshallows[0])
	})

	t.Run("full response with wanted-refs", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "acknowledgments\n")
		pktline.Writef(&buf, "ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
		pktline.WriteString(&buf, "ready\n")
		pktline.WriteDelim(&buf)
		pktline.WriteString(&buf, "wanted-refs\n")
		pktline.Writef(&buf, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/main\n")
		pktline.WriteDelim(&buf)
		pktline.WriteString(&buf, "packfile\n")
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.NotNil(t, got.WantedRefs)
		require.Len(t, got.WantedRefs.Refs, 1)
		assert.Equal(t, "refs/heads/main", got.WantedRefs.Refs[0].Name().String())
		assert.Equal(t, plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"), got.WantedRefs.Refs[0].Hash())
	})

	t.Run("packfile-uris section", func(t *testing.T) {
		t.Parallel()
		// The acknowledgments section is optional in the packfile-bearing
		// response; here it is absent and packfile-uris precedes the packfile.
		var buf bytes.Buffer
		pktline.WriteString(&buf, "packfile-uris\n")
		pktline.WriteString(&buf, "https://example.com/pack-123.pack\n")
		pktline.WriteDelim(&buf)
		pktline.WriteString(&buf, "packfile\n")
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.NotNil(t, got.PackfileURIs)
		require.Len(t, got.PackfileURIs.URIs, 1)
		assert.Equal(t, "https://example.com/pack-123.pack", got.PackfileURIs.URIs[0])
	})

	t.Run("empty response", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		assert.Nil(t, got.Acknowledgments)
		assert.Nil(t, got.ShallowInfo)
		assert.False(t, got.Packfile)
	})

	t.Run("packfile reader is live", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		pktline.WriteString(&buf, "acknowledgments\n")
		pktline.Writef(&buf, "ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n")
		pktline.WriteString(&buf, "ready\n")
		pktline.WriteDelim(&buf)
		pktline.WriteString(&buf, "packfile\n")
		// Write some packfile data
		pktline.WriteString(&buf, "PACK")

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.True(t, got.Packfile)

		// The same reader is positioned after the "packfile\n" header, so
		// the caller streams the packfile data straight from it.
		b := make([]byte, 4)
		n, err := io.ReadFull(&buf, b)
		require.NoError(t, err)
		assert.Equal(t, 4, n)
	})
}

func TestFetchOutputEncode(t *testing.T) {
	t.Parallel()

	t.Run("acknowledgments with ready", func(t *testing.T) {
		t.Parallel()
		resp := &FetchOutput{
			Acknowledgments: &Acknowledgments{
				ACKs: []plumbing.Hash{
					plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
				},
				Ready: true,
			},
			Packfile: true,
		}

		var buf bytes.Buffer
		require.NoError(t, resp.Encode(&buf))

		// Encode stops at the packfile header; append the caller's final
		// flush-pkt to form a complete response before decoding it back.
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.NotNil(t, got.Acknowledgments)
		require.Len(t, got.Acknowledgments.ACKs, 1)
		assert.True(t, got.Acknowledgments.Ready)
		assert.True(t, got.Packfile)
	})

	t.Run("negotiation round is self-terminated", func(t *testing.T) {
		t.Parallel()
		resp := &FetchOutput{
			Acknowledgments: &Acknowledgments{},
		}

		var buf bytes.Buffer
		require.NoError(t, resp.Encode(&buf))

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))
		require.NotNil(t, got.Acknowledgments)
		assert.Empty(t, got.Acknowledgments.ACKs)
		assert.False(t, got.Acknowledgments.Ready)
		assert.False(t, got.Packfile)
	})

	t.Run("ready without packfile is rejected", func(t *testing.T) {
		t.Parallel()
		resp := &FetchOutput{
			Acknowledgments: &Acknowledgments{Ready: true},
		}
		require.Error(t, resp.Encode(&bytes.Buffer{}))
	})

	t.Run("metadata without packfile is rejected", func(t *testing.T) {
		t.Parallel()
		resp := &FetchOutput{
			Acknowledgments: &Acknowledgments{},
			ShallowInfo:     &ShallowInfo{},
		}
		require.Error(t, resp.Encode(&bytes.Buffer{}))
	})

	t.Run("full response round trip", func(t *testing.T) {
		t.Parallel()
		resp := &FetchOutput{
			Acknowledgments: &Acknowledgments{
				ACKs: []plumbing.Hash{
					plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
				},
				Ready: true,
			},
			ShallowInfo: &ShallowInfo{
				Shallows:   []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
				Unshallows: []plumbing.Hash{plumbing.NewHash("2222222222222222222222222222222222222222")},
			},
			WantedRefs: &WantedRefs{
				Refs: []*plumbing.Reference{
					plumbing.NewHashReference("refs/heads/main", plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")),
				},
			},
			PackfileURIs: &PackfileURIs{
				URIs: []string{"https://example.com/pack-123.pack"},
			},
			Packfile: true,
		}

		var buf bytes.Buffer
		require.NoError(t, resp.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &FetchOutput{}
		require.NoError(t, got.Decode(&buf))

		require.NotNil(t, got.Acknowledgments)
		require.Len(t, got.Acknowledgments.ACKs, 1)
		assert.True(t, got.Acknowledgments.Ready)

		require.NotNil(t, got.ShallowInfo)
		require.Len(t, got.ShallowInfo.Shallows, 1)
		require.Len(t, got.ShallowInfo.Unshallows, 1)

		require.NotNil(t, got.WantedRefs)
		require.Len(t, got.WantedRefs.Refs, 1)

		require.NotNil(t, got.PackfileURIs)
		require.Len(t, got.PackfileURIs.URIs, 1)
		assert.True(t, got.Packfile)
	})
}

func TestFetchArgsRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("minimal", func(t *testing.T) {
		t.Parallel()
		req := &FetchArgs{
			Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
			Done:  true,
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &FetchArgs{}
		require.NoError(t, got.Decode(&buf))
		assert.Equal(t, req.Wants, got.Wants)
		assert.True(t, got.Done)
		assert.Empty(t, got.Haves)
		assert.False(t, got.ThinPack)
		assert.False(t, got.OFSDelta)
		assert.Empty(t, got.Filter)
	})

	t.Run("complete", func(t *testing.T) {
		t.Parallel()
		ts := time.Unix(1700000000, 0).UTC()
		req := &FetchArgs{
			Wants: []plumbing.Hash{
				plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
				plumbing.NewHash("a6930aaee06755d1bdcfd943fbf614e4d92bb0c7"),
			},
			Haves: []plumbing.Hash{
				plumbing.NewHash("5dc01c595e6c6ec9ccda4f6f69c131c0dd945f8c"),
			},
			Done:        true,
			ThinPack:    true,
			NoProgress:  true,
			IncludeTag:  true,
			OFSDelta:    true,
			Shallows:    []plumbing.Hash{plumbing.NewHash("3333333333333333333333333333333333333333")},
			Deepen:      10,
			DeepenSince: ts,
			DeepenNot:   []string{"refs/heads/feature"},
			Filter:      FilterBlobNone(),
			WaitForDone: true,
		}

		var buf bytes.Buffer
		require.NoError(t, req.Encode(&buf))
		pktline.WriteFlush(&buf)

		got := &FetchArgs{}
		require.NoError(t, got.Decode(&buf))
		assert.Equal(t, req.Wants, got.Wants)
		assert.Equal(t, req.Haves, got.Haves)
		assert.Equal(t, req.Done, got.Done)
		assert.Equal(t, req.ThinPack, got.ThinPack)
		assert.Equal(t, req.NoProgress, got.NoProgress)
		assert.Equal(t, req.IncludeTag, got.IncludeTag)
		assert.Equal(t, req.OFSDelta, got.OFSDelta)
		assert.Equal(t, req.Shallows, got.Shallows)
		assert.Equal(t, req.Deepen, got.Deepen)
		assert.True(t, got.DeepenSince.Equal(ts))
		assert.Equal(t, req.DeepenNot, got.DeepenNot)
		assert.Equal(t, req.Filter, got.Filter)
		assert.Equal(t, req.WaitForDone, got.WaitForDone)
	})
}
