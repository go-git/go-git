package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
func serveUploadPack(tb testing.TB, st *memory.Storage, wants []plumbing.Hash) int {
	tb.Helper()

	var upreq packp.UploadRequest
	upreq.Capabilities.Add(capability.NoProgress)
	upreq.Wants = wants

	var final packp.UploadHaves
	final.Done = true

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
