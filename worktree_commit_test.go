package git

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sgnl-ai/go-git/plumbing"
	"github.com/sgnl-ai/go-git/plumbing/cache"
	"github.com/sgnl-ai/go-git/plumbing/object"
	"github.com/sgnl-ai/go-git/plumbing/storer"
	"github.com/sgnl-ai/go-git/storage/filesystem"
	"github.com/sgnl-ai/go-git/storage/memory"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/util"
	. "gopkg.in/check.v1"
)

func (s *WorktreeSuite) TestCommitEmptyOptions(c *C) {
	fs := memfs.New()
	r, err := Init(memory.NewStorage(), fs)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	hash, err := w.Commit("foo", &CommitOptions{})
	c.Assert(err, IsNil)
	c.Assert(hash.IsZero(), Equals, false)

	commit, err := r.CommitObject(hash)
	c.Assert(err, IsNil)
	c.Assert(commit.Author.Name, Not(Equals), "")
}

func (s *WorktreeSuite) TestCommitInitial(c *C) {
	expected := plumbing.NewHash("98c4ac7c29c913f7461eae06e024dc18e80d23a4")

	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, fs)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	hash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})
	c.Assert(hash, Equals, expected)
	c.Assert(err, IsNil)

	assertStorageStatus(c, r, 1, 1, 1, expected)
}

func (s *WorktreeSuite) TestNothingToCommit(c *C) {
	expected := plumbing.NewHash("838ea833ce893e8555907e5ef224aa076f5e274a")

	r, err := Init(memory.NewStorage(), memfs.New())
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	hash, err := w.Commit("failed empty commit\n", &CommitOptions{Author: defaultSignature()})
	c.Assert(hash, Equals, plumbing.ZeroHash)
	c.Assert(err, Equals, ErrEmptyCommit)

	hash, err = w.Commit("enable empty commits\n", &CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true})
	c.Assert(hash, Equals, expected)
	c.Assert(err, IsNil)
}

func (s *WorktreeSuite) TestCommitParent(c *C) {
	expected := plumbing.NewHash("ef3ca05477530b37f48564be33ddd48063fc7a22")

	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	c.Assert(err, IsNil)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	hash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})
	c.Assert(hash, Equals, expected)
	c.Assert(err, IsNil)

	assertStorageStatus(c, s.Repository, 13, 11, 10, expected)
}

func (s *WorktreeSuite) TestCommitAll(c *C) {
	expected := plumbing.NewHash("aede6f8c9c1c7ec9ca8d287c64b8ed151276fa28")

	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	c.Assert(err, IsNil)

	util.WriteFile(fs, "LICENSE", []byte("foo"), 0644)
	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	hash, err := w.Commit("foo\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})

	c.Assert(hash, Equals, expected)
	c.Assert(err, IsNil)

	assertStorageStatus(c, s.Repository, 13, 11, 10, expected)
}

func (s *WorktreeSuite) TestRemoveAndCommitAll(c *C) {
	expected := plumbing.NewHash("907cd576c6ced2ecd3dab34a72bf9cf65944b9a9")

	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	c.Assert(err, IsNil)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)
	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	_, errFirst := w.Commit("Add in Repo\n", &CommitOptions{
		Author: defaultSignature(),
	})
	c.Assert(errFirst, IsNil)

	errRemove := fs.Remove("foo")
	c.Assert(errRemove, IsNil)

	hash, errSecond := w.Commit("Remove foo\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})
	c.Assert(errSecond, IsNil)

	c.Assert(hash, Equals, expected)
	c.Assert(err, IsNil)

	assertStorageStatus(c, s.Repository, 13, 11, 11, expected)
}

func (s *WorktreeSuite) TestCommitSign(c *C) {
	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, fs)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	_, err = w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})

	c.Assert(err, IsNil)
}

func (s *WorktreeSuite) TestCommitSignBadKey(c *C) {
	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, fs)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	_, err = w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})
}

func (s *WorktreeSuite) TestCommitTreeSort(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	_, err := Init(st, nil)
	c.Assert(err, IsNil)

	r, _ := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{
		URL: fs.Root(),
	})

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	mfs := w.Filesystem

	err = mfs.MkdirAll("delta", 0755)
	c.Assert(err, IsNil)

	for _, p := range []string{"delta_last", "Gamma", "delta/middle", "Beta", "delta-first", "alpha"} {
		util.WriteFile(mfs, p, []byte("foo"), 0644)
		_, err = w.Add(p)
		c.Assert(err, IsNil)
	}

	_, err = w.Commit("foo\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})
	c.Assert(err, IsNil)

	err = r.Push(&PushOptions{})
	c.Assert(err, IsNil)

	cmd := exec.Command("git", "fsck")
	cmd.Dir = fs.Root()
	cmd.Env = os.Environ()
	buf := &bytes.Buffer{}
	cmd.Stderr = buf
	cmd.Stdout = buf

	err = cmd.Run()

	c.Assert(err, IsNil, Commentf("%s", buf.Bytes()))
}

// https://github.com/go-git/go-git/pull/224
func (s *WorktreeSuite) TestJustStoreObjectsNotAlreadyStored(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	fsDotgit, err := fs.Chroot(".git") // real fs to get modified timestamps
	c.Assert(err, IsNil)
	storage := filesystem.NewStorage(fsDotgit, cache.NewObjectLRUDefault())

	r, err := Init(storage, fs)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	// Step 1: Write LICENSE
	util.WriteFile(fs, "LICENSE", []byte("license"), 0644)
	hLicense, err := w.Add("LICENSE")
	c.Assert(err, IsNil)
	c.Assert(hLicense, Equals, plumbing.NewHash("0484eba0d41636ba71fa612c78559cd6c3006cde"))

	hash, err := w.Commit("commit 1\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})
	c.Assert(err, IsNil)
	c.Assert(hash, Equals, plumbing.NewHash("7a7faee4630d2664a6869677cc8ab614f3fd4a18"))

	infoLicense, err := fsDotgit.Stat(filepath.Join("objects", "04", "84eba0d41636ba71fa612c78559cd6c3006cde"))
	c.Assert(err, IsNil) // checking objects file exists

	// Step 2: Write foo.
	time.Sleep(5 * time.Millisecond) // uncool, but we need to get different timestamps...
	util.WriteFile(fs, "foo", []byte("foo"), 0644)
	hFoo, err := w.Add("foo")
	c.Assert(err, IsNil)
	c.Assert(hFoo, Equals, plumbing.NewHash("19102815663d23f8b75a47e7a01965dcdc96468c"))

	hash, err = w.Commit("commit 2\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})
	c.Assert(err, IsNil)
	c.Assert(hash, Equals, plumbing.NewHash("97c0c5177e6ac57d10e8ea0017f2d39b91e2b364"))

	// Step 3: Check
	// There is no need to overwrite the object of LICENSE, because its content
	// was not changed. Just a write on the object of foo is required. This behaviour
	// is fixed by #224 and tested by comparing the timestamps of the stored objects.
	infoFoo, err := fsDotgit.Stat(filepath.Join("objects", "19", "102815663d23f8b75a47e7a01965dcdc96468c"))
	c.Assert(err, IsNil)                                                    // checking objects file exists
	c.Assert(infoLicense.ModTime().Before(infoFoo.ModTime()), Equals, true) // object of foo has another/greaterThan timestamp than LICENSE

	infoLicenseSecond, err := fsDotgit.Stat(filepath.Join("objects", "04", "84eba0d41636ba71fa612c78559cd6c3006cde"))
	c.Assert(err, IsNil)

	log.Printf("comparing mod time: %v == %v on %v (%v)", infoLicenseSecond.ModTime(), infoLicense.ModTime(), runtime.GOOS, runtime.GOARCH)
	c.Assert(infoLicenseSecond.ModTime(), Equals, infoLicense.ModTime()) // object of LICENSE should have the same timestamp because no additional write operation was performed
}

func assertStorageStatus(
	c *C, r *Repository,
	treesCount, blobCount, commitCount int, head plumbing.Hash,
) {
	trees, err := r.Storer.IterEncodedObjects(plumbing.TreeObject)
	c.Assert(err, IsNil)
	blobs, err := r.Storer.IterEncodedObjects(plumbing.BlobObject)
	c.Assert(err, IsNil)
	commits, err := r.Storer.IterEncodedObjects(plumbing.CommitObject)
	c.Assert(err, IsNil)

	c.Assert(lenIterEncodedObjects(trees), Equals, treesCount)
	c.Assert(lenIterEncodedObjects(blobs), Equals, blobCount)
	c.Assert(lenIterEncodedObjects(commits), Equals, commitCount)

	ref, err := r.Head()
	c.Assert(err, IsNil)
	c.Assert(ref.Hash(), Equals, head)
}

func lenIterEncodedObjects(iter storer.EncodedObjectIter) int {
	count := 0
	iter.ForEach(func(plumbing.EncodedObject) error {
		count++
		return nil
	})

	return count
}

func defaultSignature() *object.Signature {
	when, _ := time.Parse(object.DateFormat, "Thu May 04 00:03:43 2017 +0200")
	return &object.Signature{
		Name:  "foo",
		Email: "foo@foo.foo",
		When:  when,
	}
}
