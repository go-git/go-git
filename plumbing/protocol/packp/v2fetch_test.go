package packp

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
)

func TestV2FetchRequestEncode(t *testing.T) {
	t.Parallel()

	req := &V2FetchRequest{
		Wants:      []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Haves:      []plumbing.Hash{plumbing.NewHash("1111111111111111111111111111111111111111")},
		Done:       true,
		OFSDelta:   true,
		IncludeTag: true,
		NoProgress: true,
		Depth:      3,
		Filter:     FilterBlobNone(),
	}

	var buf bytes.Buffer
	err := req.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "command=fetch")
	assert.Contains(t, data, "agent=")
	assert.Contains(t, data, "want 6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	assert.Contains(t, data, "have 1111111111111111111111111111111111111111")
	assert.Contains(t, data, "ofs-delta")
	assert.Contains(t, data, "include-tag")
	assert.Contains(t, data, "no-progress")
	assert.Contains(t, data, "deepen 3")
	assert.Contains(t, data, "filter blob:none")
	assert.Contains(t, data, "done")
}

func TestV2FetchRequestEncodeMinimal(t *testing.T) {
	t.Parallel()

	req := &V2FetchRequest{
		Wants: []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Done:  true,
	}

	var buf bytes.Buffer
	err := req.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "command=fetch")
	assert.Contains(t, data, "want 6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	assert.Contains(t, data, "done")
	assert.NotContains(t, data, "ofs-delta")
	assert.NotContains(t, data, "thin-pack")
	assert.NotContains(t, data, "deepen")
	assert.NotContains(t, data, "filter")
}

// writeV2FetchResponse builds a V2 fetch response using proper pktline encoding.
func writeV2FetchResponse(t *testing.T, sections ...func(w io.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, s := range sections {
		s(&buf)
	}
	return buf.Bytes()
}

func ackSection(acks []string, ready bool) func(w io.Writer) {
	return func(w io.Writer) {
		pktline.Writeln(w, "acknowledgments")
		for _, a := range acks {
			pktline.Writeln(w, "ACK "+a)
		}
		if ready {
			pktline.Writeln(w, "ready")
		} else if len(acks) == 0 {
			pktline.Writeln(w, "NAK")
		}
		pktline.WriteDelim(w)
	}
}

func shallowInfoSection(shallows, unshallows []string) func(w io.Writer) {
	return func(w io.Writer) {
		pktline.Writeln(w, "shallow-info")
		for _, s := range shallows {
			pktline.Writeln(w, "shallow "+s)
		}
		for _, u := range unshallows {
			pktline.Writeln(w, "unshallow "+u)
		}
		pktline.WriteDelim(w)
	}
}

func wantedRefsSection(refs map[string]string) func(w io.Writer) {
	return func(w io.Writer) {
		pktline.Writeln(w, "wanted-refs")
		for ref, oid := range refs {
			pktline.Writeln(w, oid+" "+ref)
		}
		pktline.WriteDelim(w)
	}
}

func packfileSection() func(w io.Writer) {
	return func(w io.Writer) {
		pktline.Writeln(w, "packfile")
	}
}

func flushSection() func(w io.Writer) {
	return func(w io.Writer) {
		pktline.WriteFlush(w)
	}
}

func TestV2FetchResponseDecodeWithPackfile(t *testing.T) {
	t.Parallel()

	ackHash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	data := writeV2FetchResponse(t,
		ackSection([]string{ackHash}, true),
		packfileSection(),
	)
	// Append raw pack data after the packfile section header.
	packData := []byte("PACK-DATA-HERE")
	data = append(data, packData...)

	resp := NewV2FetchResponse()
	err := resp.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	require.Len(t, resp.ACKs, 1)
	assert.Equal(t, plumbing.NewHash(ackHash), resp.ACKs[0])
	assert.True(t, resp.Ready)

	require.NotNil(t, resp.Packfile)
	remaining, err := io.ReadAll(resp.Packfile)
	require.NoError(t, err)
	assert.Equal(t, "PACK-DATA-HERE", string(remaining))
}

func TestV2FetchResponseDecodeNAK(t *testing.T) {
	t.Parallel()

	data := writeV2FetchResponse(t,
		ackSection(nil, false),
		packfileSection(),
	)
	data = append(data, []byte("pack-data")...)

	resp := NewV2FetchResponse()
	err := resp.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	assert.Len(t, resp.ACKs, 0)
	assert.False(t, resp.Ready)
	require.NotNil(t, resp.Packfile)
}

func TestV2FetchResponseDecodeShallowInfo(t *testing.T) {
	t.Parallel()

	shallowHash := "1111111111111111111111111111111111111111"
	unshallowHash := "2222222222222222222222222222222222222222"

	data := writeV2FetchResponse(t,
		ackSection(nil, false),
		shallowInfoSection([]string{shallowHash}, []string{unshallowHash}),
		packfileSection(),
	)
	data = append(data, []byte("data")...)

	resp := NewV2FetchResponse()
	err := resp.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	require.NotNil(t, resp.ShallowUpdate)
	require.Len(t, resp.ShallowUpdate.Shallows, 1)
	assert.Equal(t, plumbing.NewHash(shallowHash), resp.ShallowUpdate.Shallows[0])
	require.Len(t, resp.ShallowUpdate.Unshallows, 1)
	assert.Equal(t, plumbing.NewHash(unshallowHash), resp.ShallowUpdate.Unshallows[0])
}

func TestV2FetchResponseDecodeWantedRefs(t *testing.T) {
	t.Parallel()

	hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	data := writeV2FetchResponse(t,
		ackSection(nil, true),
		wantedRefsSection(map[string]string{"refs/heads/main": hash}),
		packfileSection(),
	)
	data = append(data, []byte("data")...)

	resp := NewV2FetchResponse()
	err := resp.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	assert.True(t, resp.Ready)
	require.Len(t, resp.WantedRefs, 1)
	assert.Equal(t, plumbing.NewHash(hash), resp.WantedRefs["refs/heads/main"])
}

func TestV2FetchResponseEncodeAcknowledgments(t *testing.T) {
	t.Parallel()

	resp := NewV2FetchResponse()
	resp.ACKs = []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")}
	resp.Ready = true

	var buf bytes.Buffer
	err := resp.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "acknowledgments")
	assert.Contains(t, data, "ACK 6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	assert.Contains(t, data, "ready")
}

func TestV2FetchResponseDecodeNoPackfile(t *testing.T) {
	t.Parallel()

	ackHash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	data := writeV2FetchResponse(t,
		ackSection([]string{ackHash}, false),
		flushSection(),
	)

	resp := NewV2FetchResponse()
	err := resp.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	require.Len(t, resp.ACKs, 1)
	assert.False(t, resp.Ready)
	assert.Nil(t, resp.Packfile)
}

func TestV2FetchRequestEncodeWantRef(t *testing.T) {
	t.Parallel()

	req := &V2FetchRequest{
		WantRefs: []string{"refs/heads/main", "refs/heads/feature"},
		Done:     true,
	}

	var buf bytes.Buffer
	err := req.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "command=fetch")
	assert.Contains(t, data, "want-ref refs/heads/main")
	assert.Contains(t, data, "want-ref refs/heads/feature")
	assert.Contains(t, data, "done")
	assert.NotContains(t, data, "want ")
}

func TestV2FetchRequestEncodeWaitForDone(t *testing.T) {
	t.Parallel()

	req := &V2FetchRequest{
		Wants:       []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Done:        true,
		WaitForDone: true,
	}

	var buf bytes.Buffer
	err := req.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "wait-for-done")
}

func TestV2FetchRequestEncodePackfileURIs(t *testing.T) {
	t.Parallel()

	req := &V2FetchRequest{
		Wants:                []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Done:                 true,
		PackfileURIProtocols: []string{"https", "http"},
	}

	var buf bytes.Buffer
	err := req.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "packfile-uris https,http")
}

func TestV2FetchRequestEncodeSidebandAll(t *testing.T) {
	t.Parallel()

	req := &V2FetchRequest{
		Wants:       []plumbing.Hash{plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")},
		Done:        true,
		SidebandAll: true,
	}

	var buf bytes.Buffer
	err := req.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "sideband-all")
}

func TestV2FetchResponseDecodeMultipleWantedRefs(t *testing.T) {
	t.Parallel()

	hash := "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"

	data := writeV2FetchResponse(t,
		ackSection(nil, true),
		wantedRefsSection(map[string]string{
			"refs/heads/main":    hash,
			"refs/heads/feature": hash,
		}),
		packfileSection(),
	)
	data = append(data, []byte("data")...)

	resp := NewV2FetchResponse()
	err := resp.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	assert.True(t, resp.Ready)
	require.Len(t, resp.WantedRefs, 2)
	assert.Equal(t, plumbing.NewHash(hash), resp.WantedRefs["refs/heads/main"])
	assert.Equal(t, plumbing.NewHash(hash), resp.WantedRefs["refs/heads/feature"])
}

func packfileURIsSection(uris map[string]string) func(w io.Writer) {
	return func(w io.Writer) {
		pktline.Writeln(w, "packfile-uris")
		for hash, uri := range uris {
			pktline.Writeln(w, hash+" "+uri)
		}
		pktline.WriteDelim(w)
	}
}

func TestV2FetchResponseDecodePackfileURIs(t *testing.T) {
	t.Parallel()

	hash1 := "1111111111111111111111111111111111111111"
	hash2 := "2222222222222222222222222222222222222222"

	data := writeV2FetchResponse(t,
		ackSection(nil, true),
		packfileURIsSection(map[string]string{
			hash1: "https://cdn.example.com/pack1.pack",
			hash2: "https://cdn.example.com/pack2.pack",
		}),
		packfileSection(),
	)
	data = append(data, []byte("data")...)

	resp := NewV2FetchResponse()
	err := resp.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	require.Len(t, resp.PackfileURIs, 2)

	uriMap := make(map[string]string)
	for _, pu := range resp.PackfileURIs {
		uriMap[pu.Hash.String()] = pu.URI
	}
	assert.Equal(t, "https://cdn.example.com/pack1.pack", uriMap[hash1])
	assert.Equal(t, "https://cdn.example.com/pack2.pack", uriMap[hash2])
}

func TestV2FetchResponseEncodePackfileURIs(t *testing.T) {
	t.Parallel()

	resp := NewV2FetchResponse()
	resp.Ready = true
	resp.PackfileURIs = []V2PackfileURI{
		{Hash: plumbing.NewHash("1111111111111111111111111111111111111111"), URI: "https://cdn.example.com/pack.pack"},
	}
	resp.Packfile = bytes.NewReader(nil)

	var buf bytes.Buffer
	err := resp.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "packfile-uris")
	assert.Contains(t, data, "1111111111111111111111111111111111111111 https://cdn.example.com/pack.pack")
	assert.Contains(t, data, "packfile")
}

func TestV2FetchResponseEncodeWantedRefs(t *testing.T) {
	t.Parallel()

	resp := NewV2FetchResponse()
	resp.Ready = true
	resp.WantedRefs = map[string]plumbing.Hash{
		"refs/heads/main": plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	}
	resp.Packfile = bytes.NewReader(nil)

	var buf bytes.Buffer
	err := resp.Encode(&buf)
	require.NoError(t, err)

	data := buf.String()
	assert.Contains(t, data, "wanted-refs")
	assert.Contains(t, data, "6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/main")
}
