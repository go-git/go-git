package object

import (
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
)

func FuzzCommitDecode(f *testing.F) {
	f.Add([]byte(
		"tree 0000000000000000000000000000000000000000\n" +
			"author a <a> 0 +0000\n" +
			"committer c <c> 0 +0000\n" +
			"\n" +
			"msg\n",
	))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		mo := &plumbing.MemoryObject{}
		mo.SetType(plumbing.CommitObject)
		_, _ = mo.Write(data)
		_ = (&Commit{}).Decode(mo)
	})
}

func FuzzTreeDecode(f *testing.F) {
	// "100644 a\x00" followed by 20 zero bytes (SHA-1 hash).
	f.Add(append([]byte("100644 a\x00"), make([]byte, 20)...))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		mo := &plumbing.MemoryObject{}
		mo.SetType(plumbing.TreeObject)
		_, _ = mo.Write(data)

		var tree Tree
		if tree.Decode(mo) != nil {
			return
		}
		for _, e := range tree.Entries {
			if e.Mode.IsMalformed() || e.Mode == filemode.Deprecated {
				t.Fatalf("entry %q decoded to non-canonical mode %o", e.Name, e.Mode)
			}
		}
	})
}

func FuzzTagDecode(f *testing.F) {
	f.Add([]byte(
		"object 0000000000000000000000000000000000000000\n" +
			"type commit\n" +
			"tag v1\n" +
			"tagger t <t> 0 +0000\n" +
			"\n" +
			"msg\n",
	))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		mo := &plumbing.MemoryObject{}
		mo.SetType(plumbing.TagObject)
		_, _ = mo.Write(data)
		_ = (&Tag{}).Decode(mo)
	})
}

func FuzzBlobDecode(f *testing.F) {
	f.Add([]byte("hello\x00world\x01\x02"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		mo := &plumbing.MemoryObject{}
		mo.SetType(plumbing.BlobObject)
		_, _ = mo.Write(data)
		_ = (&Blob{}).Decode(mo)
	})
}
