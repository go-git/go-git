package commitgraph

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// FuzzDecodeTraversalCommit feeds arbitrary commit bodies to the
// lightweight traversal decoder under both object formats, and checks
// that whatever it accepts agrees with object.Commit on the fields
// traversal relies on.
func FuzzDecodeTraversalCommit(f *testing.F) {
	// Helpers are inlined because OSS-Fuzz extracts the FuzzXxx target
	// into a standalone non-test file and strips the rest of _test.go.

	// traversalWhen maps the zero time object.Commit reports for a
	// missing or unparsable date to the Unix epoch the traversal
	// decoder falls back to.
	traversalWhen := func(t time.Time) time.Time {
		if t.IsZero() {
			return time.Unix(0, 0)
		}
		return t
	}

	sha1Hex := strings.Repeat("a", 40)
	sha256Hex := strings.Repeat("b", 64)
	f.Add([]byte("tree " + sha1Hex + "\nparent " + sha1Hex + "\nparent " + sha1Hex +
		"\nauthor A <a> 1136239445 -0700\ncommitter C <c> 1136300000 +0530\n\nmsg\n"))
	f.Add([]byte("tree " + sha256Hex + "\nparent " + sha256Hex +
		"\nauthor A <a> 1 +0000\ncommitter C <c> 2 +0000\ngpgsig x\n y\n\nmsg\n"))
	// Truncated, unterminated and malformed headers.
	f.Add([]byte("tree " + sha1Hex))
	f.Add([]byte("tree " + sha1Hex + "\nparent " + sha1Hex))
	f.Add([]byte("tree " + sha1Hex + "\nparent " + sha1Hex[:39] + "\n"))
	f.Add([]byte("tree " + sha1Hex + "\nauthor A <a> notatime +0000\ncommitter C <c> 2 +9x00\n"))
	f.Add([]byte("tree " + sha1Hex + "\ncommitter C <c> 2 +0000\nauthor A <a> 1 +0000\n"))
	f.Add([]byte("tree " + sha1Hex + "\nauthor A <a> 99999999999999999999 +0000\n"))
	f.Add([]byte("tree " + sha1Hex + "\nauthor >01\ncommitter C c> 2 +0000\n"))
	f.Add([]byte("parent " + sha1Hex + "\ntree " + sha1Hex + "\n"))
	// A header longer than the pooled bufio.Reader's buffer.
	f.Add([]byte("tree " + sha1Hex + "\nauthor " + strings.Repeat("x", 8192) + " <a> 1 +0000\n"))
	f.Add([]byte("\n"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		obj := &plumbing.MemoryObject{}
		obj.SetType(plumbing.CommitObject)
		_, _ = obj.Write(data)

		for _, of := range []formatcfg.ObjectFormat{formatcfg.SHA1, formatcfg.SHA256} {
			id := plumbing.NewHash(strings.Repeat("0", of.HexSize()))
			light, lightErr := decodeTraversalCommit(obj, id)
			if lightErr == nil {
				for _, h := range append([]plumbing.Hash{light.Tree()}, light.Parents()...) {
					if h.Size() != of.Size() {
						t.Fatalf("%s: decoded %d-byte hash %s", of, h.Size(), h)
					}
				}
			}

			// The traversal decoder stops after the committer header, so
			// it may accept commits that object.Commit rejects later on.
			full, err := object.DecodeCommit(memory.NewStorage(), obj)
			if err != nil {
				continue
			}
			if lightErr != nil {
				// The only commits object.Commit accepts and the traversal
				// decoder rejects are those with tree or parent IDs of the
				// other object format's width.
				sameWidth := full.TreeHash.Size() == of.Size()
				for _, p := range full.ParentHashes {
					sameWidth = sameWidth && p.Size() == of.Size()
				}
				if sameWidth {
					t.Fatalf("%s: traversal rejected a commit object.Commit accepts: %v", of, lightErr)
				}
				continue
			}
			if full.TreeHash != light.Tree() {
				t.Errorf("%s: tree = %s, object.Commit has %s", of, light.Tree(), full.TreeHash)
			}
			if !slices.Equal(full.ParentHashes, light.Parents()) {
				t.Errorf("%s: parents = %v, object.Commit has %v", of, light.Parents(), full.ParentHashes)
			}
			if !traversalWhen(full.Author.When).Equal(light.AuthorWhen()) {
				t.Errorf("%s: author time = %v, object.Commit has %v", of, light.AuthorWhen(), full.Author.When)
			}
			if !traversalWhen(full.Committer.When).Equal(light.When()) {
				t.Errorf("%s: committer time = %v, object.Commit has %v", of, light.When(), full.Committer.When)
			}
		}
	})
}
