//go:build sha256
// +build sha256

package object

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestDecodeCommitSHA256ObjectIDs(t *testing.T) {
	const (
		treeID   = "151c11736080ee7fe882040a334ec2ae4a815deca7a3d63374500e107dbcda09"
		parentID = "36561e87343747f0bc493eeeb04d910e5382c3419c42d412687e2aac5e4c40a1"
		raw      = "tree " + treeID + "\n" +
			"parent " + parentID + "\n" +
			"author Foo <foo@example.local> 1427802494 +0200\n" +
			"committer Foo <foo@example.local> 1427802494 +0200\n\n" +
			"msg\n"
	)

	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.CommitObject)
	if _, err := obj.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}

	commit := &Commit{}
	if err := commit.Decode(obj); err != nil {
		t.Fatal(err)
	}

	if got := commit.TreeHash.String(); got != treeID {
		t.Fatalf("TreeHash = %q, want %q", got, treeID)
	}
	if len(commit.ParentHashes) != 1 {
		t.Fatalf("ParentHashes length = %d, want 1", len(commit.ParentHashes))
	}
	if got := commit.ParentHashes[0].String(); got != parentID {
		t.Fatalf("ParentHashes[0] = %q, want %q", got, parentID)
	}
}
