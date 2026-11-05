package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// benchRepo builds a linear history of commits commits, then creates refs
// references pointing at commits spread across that history, so that the
// wants share most of their ancestry. This is the shape of a server hosting
// a repository with many branches over a common history.
func benchRepo(tb testing.TB, commits, refs int) (*memory.Storage, []plumbing.Hash) {
	tb.Helper()

	st := memory.NewStorage()
	sig := object.Signature{Name: "bench", Email: "bench@example.com", When: time.Unix(0, 0).UTC()}

	var parent plumbing.Hash
	history := make([]plumbing.Hash, 0, commits)

	for i := range commits {
		blob := &plumbing.MemoryObject{}
		blob.SetType(plumbing.BlobObject)
		if _, err := fmt.Fprintf(blob, "content-%d", i); err != nil {
			tb.Fatal(err)
		}
		bh, err := st.SetEncodedObject(blob)
		if err != nil {
			tb.Fatal(err)
		}

		tree := &object.Tree{Entries: []object.TreeEntry{
			{Name: "file.txt", Mode: filemode.Regular, Hash: bh},
		}}
		to := &plumbing.MemoryObject{}
		if err := tree.Encode(to); err != nil {
			tb.Fatal(err)
		}
		th, err := st.SetEncodedObject(to)
		if err != nil {
			tb.Fatal(err)
		}

		c := &object.Commit{Author: sig, Committer: sig, Message: "commit", TreeHash: th}
		if !parent.IsZero() {
			c.ParentHashes = []plumbing.Hash{parent}
		}
		co := &plumbing.MemoryObject{}
		if err := c.Encode(co); err != nil {
			tb.Fatal(err)
		}
		ch, err := st.SetEncodedObject(co)
		if err != nil {
			tb.Fatal(err)
		}

		parent = ch
		history = append(history, ch)
	}

	wants := make([]plumbing.Hash, 0, refs)
	for i := range refs {
		idx := max(commits-1-(i*commits/(refs*2)), 0)
		h := history[idx]

		name := plumbing.ReferenceName(fmt.Sprintf("refs/heads/branch-%d", i))
		if err := st.SetReference(plumbing.NewHashReference(name, h)); err != nil {
			tb.Fatal(err)
		}
		wants = append(wants, h)
	}

	if err := st.SetReference(plumbing.NewHashReference(plumbing.HEAD, history[commits-1])); err != nil {
		tb.Fatal(err)
	}

	return st, wants
}

// serveUploadPack runs a full upload-pack exchange for the given wants and
// returns the number of bytes written to the client. The response is not
// copied out of the buffer, so that the measurement stays on UploadPack
// itself rather than on materialising the packfile a second time.
func serveUploadPack(tb testing.TB, st *memory.Storage, wants []plumbing.Hash, haves ...plumbing.Hash) int {
	tb.Helper()

	var upreq packp.UploadRequest
	upreq.Capabilities.Add(capability.NoProgress)
	upreq.Wants = wants

	var final packp.UploadHaves
	final.Done = true
	final.Haves = haves

	var req bytes.Buffer
	if err := upreq.Encode(&req); err != nil {
		tb.Fatal(err)
	}
	if err := final.Encode(&req); err != nil {
		tb.Fatal(err)
	}

	var out bytes.Buffer
	if err := UploadPack(context.Background(), st,
		io.NopCloser(&req), ioutil.WriteNopCloser(&out),
		&UploadPackRequest{GitProtocol: "version=1"}); err != nil {
		tb.Fatal(err)
	}

	return out.Len()
}

func benchmarkUploadPack(b *testing.B, commits, wants int) {
	st, w := benchRepo(b, commits, wants)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if serveUploadPack(b, st, w) == 0 {
			b.Fatal("no output produced")
		}
	}
}

// The cost of serving a fetch should depend on the size of the history, not on
// how many refs the client asks for.
func BenchmarkUploadPackWants1(b *testing.B)   { benchmarkUploadPack(b, 300, 1) }
func BenchmarkUploadPackWants16(b *testing.B)  { benchmarkUploadPack(b, 300, 16) }
func BenchmarkUploadPackWants64(b *testing.B)  { benchmarkUploadPack(b, 300, 64) }
func BenchmarkUploadPackWants256(b *testing.B) { benchmarkUploadPack(b, 300, 256) }

func benchmarkUploadPackHaveNearTip(b *testing.B, commits int) {
	st, wants := benchRepo(b, commits, 1)
	tip, err := object.GetCommit(st, wants[0])
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if serveUploadPack(b, st, wants, tip.ParentHashes[0]) == 0 {
			b.Fatal("no output produced")
		}
	}
}

// A fetch that is one commit behind should not cost more on a longer history.
func BenchmarkUploadPackHaveNearTip300(b *testing.B)  { benchmarkUploadPackHaveNearTip(b, 300) }
func BenchmarkUploadPackHaveNearTip3000(b *testing.B) { benchmarkUploadPackHaveNearTip(b, 3000) }

// benchCommit stores a commit with the given parents, dated when, and
// returns its hash.
func benchCommit(tb testing.TB, st *memory.Storage, when int64, parents ...plumbing.Hash) plumbing.Hash {
	tb.Helper()
	sig := object.Signature{Name: "bench", Email: "bench@example.com", When: time.Unix(when, 0).UTC()}
	c := &object.Commit{Author: sig, Committer: sig, Message: fmt.Sprintf("commit %d", when), TreeHash: plumbing.ZeroHash, ParentHashes: parents}
	o := &plumbing.MemoryObject{}
	if err := c.Encode(o); err != nil {
		tb.Fatal(err)
	}
	h, err := st.SetEncodedObject(o)
	if err != nil {
		tb.Fatal(err)
	}
	return h
}

// BenchmarkCommonHavesManyCommonHaves measures negotiation when a stateless
// request names many commits the server has that the want does not reach,
// each followed by one it lacks: every new common commit lowers the cutoff,
// so each answer needs a walk from the want.
func BenchmarkCommonHavesManyCommonHaves(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("commits=%d", n), func(b *testing.B) {
			st := memory.NewStorage()
			root := benchCommit(b, st, 1)
			want, have := root, root
			haves := make([]plumbing.Hash, 0, n)
			for i := range n {
				want = benchCommit(b, st, int64(2*i+2), want)
				have = benchCommit(b, st, int64(2*i+3), have)
				haves = append(haves, have)
			}
			unknown := plumbing.NewHash("1111111111111111111111111111111111111111")

			ctx := context.Background()
			b.ReportAllocs()
			for b.Loop() {
				c := mustCommonHaves(b, st, []plumbing.Hash{want}, nil)
				for _, h := range slices.Backward(haves) {
					if _, err := c.add(h); err != nil {
						b.Fatal(err)
					}
					if _, err := c.add(unknown); err != nil {
						b.Fatal(err)
					}
					if _, err := c.okToGiveUp(ctx); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
