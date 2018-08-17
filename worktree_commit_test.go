package git

import (
	"bytes"
	"strings"
	"time"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/storage/memory"

	. "gopkg.in/check.v1"
	"gopkg.in/src-d/go-billy.v4/memfs"
	"gopkg.in/src-d/go-billy.v4/util"
)

func (s *WorktreeSuite) TestCommitInvalidOptions(c *C) {
	r, err := Init(memory.NewStorage(), memfs.New())
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	hash, err := w.Commit("", &CommitOptions{})
	c.Assert(err, Equals, ErrMissingAuthor)
	c.Assert(hash.IsZero(), Equals, true)
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
	// expectedHash := plumbing.NewHash("98c4ac7c29c913f7461eae06e024dc18e80d23a4")

	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, fs)
	c.Assert(err, IsNil)

	w, err := r.Worktree()
	c.Assert(err, IsNil)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	c.Assert(err, IsNil)

	key := commitSignKey(c)
	hash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature(), SignKey: key})
	// c.Assert(hash, Equals, expectedHash)
	c.Assert(err, IsNil)

	// assertStorageStatus(c, r, 1, 1, 1, expectedHash)

	// Verify the commit.
	pks := new(bytes.Buffer)
	pkw, err := armor.Encode(pks, openpgp.PublicKeyType, nil)
	c.Assert(err, IsNil)

	err = key.Serialize(pkw)
	c.Assert(err, IsNil)
	err = pkw.Close()
	c.Assert(err, IsNil)

	expectedCommit, err := r.CommitObject(hash)
	c.Assert(err, IsNil)
	actual, err := expectedCommit.Verify(pks.String())
	c.Assert(err, IsNil)
	c.Assert(actual.PrimaryKey, DeepEquals, key.PrimaryKey)
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

func commitSignKey(c *C) *openpgp.Entity {
	s := strings.NewReader(armoredKeyRing)
	es, err := openpgp.ReadArmoredKeyRing(s)
	c.Assert(err, IsNil)

	c.Assert(es, HasLen, 1)
	c.Assert(es[0].Identities, HasLen, 1)
	_, ok := es[0].Identities["foo bar <foo@foo.foo>"]
	c.Assert(ok, Equals, true)

	return es[0]
}

const armoredKeyRing = `
-----BEGIN PGP PRIVATE KEY BLOCK-----

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
=bnr6
-----END PGP PRIVATE KEY BLOCK-----
`
