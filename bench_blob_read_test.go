package git

import (
	"io"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// BenchmarkBlobRead opens the current repo, resolves commit 9d0f15c4,
// walks its tree, and reads every blob.
func BenchmarkBlobRead(b *testing.B) {
	// v5.0.0 tag. This is now 6 years ago, so reconstructing this
	// will follow a lot delta chains.
	const commitSHA = "9d0f15c4fa712cdacfa3887e9baac918f093fbf6"

	for b.Loop() {
		commitHash := plumbing.NewHash(commitSHA)

		repo, err := PlainOpen(".")
		if err != nil {
			b.Fatal(err)
		}

		commit, err := repo.CommitObject(commitHash)
		if err != nil {
			b.Fatal(err)
		}

		tree, err := commit.Tree()
		if err != nil {
			b.Fatal(err)
		}

		iter := object.NewFileIter(repo.Storer, tree)
		for {
			f, err := iter.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatal(err)
			}

			r, err := f.Reader()
			if err != nil {
				b.Fatal(err)
			}
			if _, err := io.Copy(io.Discard, r); err != nil {
				r.Close()
				b.Fatal(err)
			}
			r.Close()
		}
	}
}
