package git

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/util"
)

func (s *WorktreeSuite) TestCommitEmptyOptions() {
	fs := memfs.New()
	r, err := Init(memory.NewStorage(), WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	s.NoError(err)

	hash, err := w.Commit("foo", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)
	s.False(hash.IsZero())

	commit, err := r.CommitObject(hash)
	s.NoError(err)
	s.NotEqual("", commit.Author.Name)
}

func (s *WorktreeSuite) TestCommitInitial() {
	expected := plumbing.NewHash("98c4ac7c29c913f7461eae06e024dc18e80d23a4")

	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	s.NoError(err)

	hash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})
	s.Equal(expected, hash)
	s.NoError(err)

	assertStorageStatus(s, r, 1, 1, 1, expected)
}

func (s *WorktreeSuite) TestNothingToCommit() {
	expected := plumbing.NewHash("838ea833ce893e8555907e5ef224aa076f5e274a")

	r, err := Init(memory.NewStorage(), WithWorkTree(memfs.New()))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	hash, err := w.Commit("failed empty commit\n", &CommitOptions{Author: defaultSignature()})
	s.Equal(plumbing.ZeroHash, hash)
	s.ErrorIs(err, ErrEmptyCommit)

	hash, err = w.Commit("enable empty commits\n", &CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true})
	s.Equal(expected, hash)
	s.NoError(err)
}

func (s *WorktreeSuite) TestNothingToCommitNonEmptyRepo() {
	fs := memfs.New()
	r, err := Init(memory.NewStorage(), WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	err = util.WriteFile(fs, "foo", []byte("foo"), 0644)
	s.NoError(err)

	w.Add("foo")
	_, err = w.Commit("previous commit\n", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	hash, err := w.Commit("failed empty commit\n", &CommitOptions{Author: defaultSignature()})
	s.Equal(plumbing.ZeroHash, hash)
	s.ErrorIs(err, ErrEmptyCommit)

	_, err = w.Commit("enable empty commits\n", &CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true})
	s.NoError(err)
}

func (s *WorktreeSuite) TestRemoveAndCommitToMakeEmptyRepo() {
	fs := memfs.New()
	r, err := Init(memory.NewStorage(), WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	err = util.WriteFile(fs, "foo", []byte("foo"), 0644)
	s.NoError(err)

	_, err = w.Add("foo")
	s.NoError(err)

	_, err = w.Commit("Add in Repo\n", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	err = fs.Remove("foo")
	s.NoError(err)

	_, err = w.Add("foo")
	s.NoError(err)

	_, err = w.Commit("Remove foo\n", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)
}

func (s *WorktreeSuite) TestCommitParent() {
	expected := plumbing.NewHash("ef3ca05477530b37f48564be33ddd48063fc7a22")

	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = util.WriteFile(fs, "foo", []byte("foo"), 0644)
	s.NoError(err)

	_, err = w.Add("foo")
	s.NoError(err)

	hash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})
	s.Equal(expected, hash)
	s.NoError(err)

	assertStorageStatus(s, s.Repository, 13, 11, 10, expected)
}

func (s *WorktreeSuite) TestCommitAmendWithoutChanges() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = util.WriteFile(fs, "foo", []byte("foo"), 0644)
	s.NoError(err)

	_, err = w.Add("foo")
	s.NoError(err)

	prevHash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	amendedHash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature(), Amend: true})
	s.NoError(err)

	headRef, err := w.r.Head()
	s.NoError(err)

	s.Equal(headRef.Hash(), amendedHash)
	s.Equal(prevHash, amendedHash)

	commit, err := w.r.CommitObject(headRef.Hash())
	s.NoError(err)
	s.Equal("foo\n", commit.Message)

	assertStorageStatus(s, s.Repository, 13, 11, 10, amendedHash)
}

func (s *WorktreeSuite) TestCommitAmendWithChanges() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	s.NoError(err)

	_, err = w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	util.WriteFile(fs, "bar", []byte("bar"), 0644)

	_, err = w.Add("bar")
	s.NoError(err)

	amendedHash, err := w.Commit("bar\n", &CommitOptions{Author: defaultSignature(), Amend: true})
	s.NoError(err)

	headRef, err := w.r.Head()
	s.NoError(err)

	s.Equal(headRef.Hash(), amendedHash)

	commit, err := w.r.CommitObject(headRef.Hash())
	s.NoError(err)
	s.Equal("bar\n", commit.Message)
	s.Equal(1, commit.NumParents())

	stats, err := commit.Stats()
	s.NoError(err)
	s.Len(stats, 2)
	s.Equal(object.FileStat{
		Name:     "bar",
		Addition: 1,
	}, stats[0])
	s.Equal(object.FileStat{
		Name:     "foo",
		Addition: 1,
	}, stats[1])

	assertStorageStatus(s, s.Repository, 14, 12, 11, amendedHash)
}

func (s *WorktreeSuite) TestCommitAmendNothingToCommit() {
	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	err = util.WriteFile(fs, "foo", []byte("foo"), 0644)
	s.NoError(err)

	_, err = w.Add("foo")
	s.NoError(err)

	prevHash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature()})
	s.NoError(err)

	_, err = w.Commit("bar\n", &CommitOptions{Author: defaultSignature(), AllowEmptyCommits: true})
	s.NoError(err)

	amendedHash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature(), Amend: true})
	s.T().Log(prevHash, amendedHash)
	s.ErrorIs(err, ErrEmptyCommit)
	s.Equal(plumbing.ZeroHash, amendedHash)
}

func TestCount(t *testing.T) {
	f := fixtures.Basic().One()
	r := NewRepositoryWithEmptyWorktree(f)

	iter, err := r.CommitObjects()
	require.NoError(t, err)

	count := 0
	iter.ForEach(func(c *object.Commit) error {
		count++
		return nil
	})
	assert.Equal(t, 9, count, "commits mismatch")

	trees, err := r.TreeObjects()
	require.NoError(t, err)

	count = 0
	trees.ForEach(func(c *object.Tree) error {
		count++
		return nil
	})
	assert.Equal(t, 12, count, "trees mismatch")

	blobs, err := r.BlobObjects()
	require.NoError(t, err)

	count = 0
	blobs.ForEach(func(c *object.Blob) error {
		count++
		return nil
	})
	assert.Equal(t, 10, count, "blobs mismatch")

	objects, err := r.Objects()
	require.NoError(t, err)

	count = 0
	objects.ForEach(func(c object.Object) error {
		count++
		return nil
	})
	assert.Equal(t, 31, count, "objects mismatch")
}

func TestAddAndCommitWithSkipStatus(t *testing.T) {
	expected := plumbing.NewHash("375a3808ffde7f129cdd3c8c252fd0fe37cfd13b")

	f := fixtures.Basic().One()
	fs := memfs.New()
	r := NewRepositoryWithEmptyWorktree(f)
	w := &Worktree{
		r:          r,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	require.NoError(t, err)

	util.WriteFile(fs, "LICENSE", []byte("foo"), 0644)
	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	err = w.AddWithOptions(&AddOptions{
		Path:       "foo",
		SkipStatus: true,
	})
	require.NoError(t, err)

	hash, err := w.Commit("commit foo only\n", &CommitOptions{
		Author: defaultSignature(),
	})

	assert.Equal(t, expected.String(), hash.String())
	require.NoError(t, err)

	assertStorage(t, r, 13, 11, 10, expected)
}

func assertStorage(
	t *testing.T, r *Repository,
	treesCount, blobCount, commitCount int, head plumbing.Hash,
) {
	trees, err := r.Storer.IterEncodedObjects(plumbing.TreeObject)
	require.NoError(t, err)
	blobs, err := r.Storer.IterEncodedObjects(plumbing.BlobObject)
	require.NoError(t, err)
	commits, err := r.Storer.IterEncodedObjects(plumbing.CommitObject)
	require.NoError(t, err)

	assert.Equal(t, treesCount, lenIterEncodedObjects(trees), "trees count mismatch")
	assert.Equal(t, blobCount, lenIterEncodedObjects(blobs), "blobs count mismatch")
	assert.Equal(t, commitCount, lenIterEncodedObjects(commits), "commits count mismatch")

	ref, err := r.Head()
	require.NoError(t, err)
	assert.Equal(t, head.String(), ref.Hash().String())
}

func (s *WorktreeSuite) TestAddAndCommitWithSkipStatusPathNotModified() {
	expected := plumbing.NewHash("375a3808ffde7f129cdd3c8c252fd0fe37cfd13b")
	expected2 := plumbing.NewHash("8691273baf8f6ee2cccfc05e910552c04d02d472")

	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	status, err := w.Status()
	s.NoError(err)
	foo := status.File("foo")
	s.Equal(Untracked, foo.Staging)
	s.Equal(Untracked, foo.Worktree)

	err = w.AddWithOptions(&AddOptions{
		Path:       "foo",
		SkipStatus: true,
	})
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	foo = status.File("foo")
	s.Equal(Added, foo.Staging)
	s.Equal(Unmodified, foo.Worktree)

	hash, err := w.Commit("commit foo only\n", &CommitOptions{All: true,
		Author: defaultSignature(),
	})
	s.Equal(expected, hash)
	s.NoError(err)

	commit1, err := w.r.CommitObject(hash)
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	foo = status.File("foo")
	s.Equal(Untracked, foo.Staging)
	s.Equal(Untracked, foo.Worktree)

	assertStorageStatus(s, s.Repository, 13, 11, 10, expected)

	err = w.AddWithOptions(&AddOptions{
		Path:       "foo",
		SkipStatus: true,
	})
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	foo = status.File("foo")
	s.Equal(Untracked, foo.Staging)
	s.Equal(Untracked, foo.Worktree)

	hash, err = w.Commit("commit with no changes\n", &CommitOptions{
		Author:            defaultSignature(),
		AllowEmptyCommits: true,
	})
	s.Equal(expected2, hash)
	s.NoError(err)

	commit2, err := w.r.CommitObject(hash)
	s.NoError(err)

	status, err = w.Status()
	s.NoError(err)
	foo = status.File("foo")
	s.Equal(Untracked, foo.Staging)
	s.Equal(Untracked, foo.Worktree)

	patch, err := commit2.Patch(commit1)
	s.NoError(err)
	files := patch.FilePatches()
	s.Nil(files)

	assertStorageStatus(s, s.Repository, 13, 11, 11, expected2)
}

func (s *WorktreeSuite) TestCommitAll() {
	expected := plumbing.NewHash("aede6f8c9c1c7ec9ca8d287c64b8ed151276fa28")

	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	util.WriteFile(fs, "LICENSE", []byte("foo"), 0644)
	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	hash, err := w.Commit("foo\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})

	s.Equal(expected, hash)
	s.NoError(err)

	assertStorageStatus(s, s.Repository, 13, 11, 10, expected)
}

func (s *WorktreeSuite) TestRemoveAndCommitAll() {
	expected := plumbing.NewHash("907cd576c6ced2ecd3dab34a72bf9cf65944b9a9")

	fs := memfs.New()
	w := &Worktree{
		r:          s.Repository,
		Filesystem: fs,
	}

	err := w.Checkout(&CheckoutOptions{})
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)
	_, err = w.Add("foo")
	s.NoError(err)

	_, errFirst := w.Commit("Add in Repo\n", &CommitOptions{
		Author: defaultSignature(),
	})
	s.Nil(errFirst)

	errRemove := fs.Remove("foo")
	s.Nil(errRemove)

	hash, errSecond := w.Commit("Remove foo\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})
	s.Nil(errSecond)

	s.Equal(expected, hash)
	s.NoError(err)

	assertStorageStatus(s, s.Repository, 13, 11, 11, expected)
}

func (s *WorktreeSuite) TestCommitSign() {
	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	s.NoError(err)

	key := commitSignKey(s.T(), true)
	hash, err := w.Commit("foo\n", &CommitOptions{Author: defaultSignature(), SignKey: key})
	s.NoError(err)

	// Verify the commit.
	pks := new(bytes.Buffer)
	pkw, err := armor.Encode(pks, openpgp.PublicKeyType, nil)
	s.NoError(err)

	err = key.Serialize(pkw)
	s.NoError(err)
	err = pkw.Close()
	s.NoError(err)

	expectedCommit, err := r.CommitObject(hash)
	s.NoError(err)
	actual, err := expectedCommit.Verify(pks.String())
	s.NoError(err)
	s.Equal(key.PrimaryKey, actual.PrimaryKey)
}

func (s *WorktreeSuite) TestCommitSignBadKey() {
	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	s.NoError(err)

	key := commitSignKey(s.T(), false)
	_, err = w.Commit("foo\n", &CommitOptions{Author: defaultSignature(), SignKey: key})
	s.ErrorIs(err, errors.InvalidArgumentError("signing key is encrypted"))
}

func (s *WorktreeSuite) TestCommitTreeSort() {
	fs := s.TemporalFilesystem()

	st := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())
	_, err := Init(st)
	s.NoError(err)

	r, _ := Clone(memory.NewStorage(), memfs.New(), &CloneOptions{
		URL: fs.Root(),
	})

	w, err := r.Worktree()
	s.NoError(err)

	mfs := w.Filesystem

	err = mfs.MkdirAll("delta", 0755)
	s.NoError(err)

	for _, p := range []string{"delta_last", "Gamma", "delta/middle", "Beta", "delta-first", "alpha"} {
		util.WriteFile(mfs, p, []byte("foo"), 0644)
		_, err = w.Add(p)
		s.NoError(err)
	}

	_, err = w.Commit("foo\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})
	s.NoError(err)

	err = r.Push(&PushOptions{})
	s.NoError(err)

	cmd := exec.Command("git", "fsck")
	cmd.Dir = fs.Root()
	cmd.Env = os.Environ()
	buf := &bytes.Buffer{}
	cmd.Stderr = buf
	cmd.Stdout = buf

	err = cmd.Run()

	s.NoError(err, fmt.Sprintf("%s", buf.Bytes()))
}

// https://github.com/go-git/go-git/pull/224
func (s *WorktreeSuite) TestJustStoreObjectsNotAlreadyStored() {
	fs := s.TemporalFilesystem()

	fsDotgit, err := fs.Chroot(".git") // real fs to get modified timestamps
	s.NoError(err)
	storage := filesystem.NewStorage(fsDotgit, cache.NewObjectLRUDefault())

	r, err := Init(storage, WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	// Step 1: Write LICENSE
	util.WriteFile(fs, "LICENSE", []byte("license"), 0644)
	hLicense, err := w.Add("LICENSE")
	s.NoError(err)
	s.Equal(plumbing.NewHash("0484eba0d41636ba71fa612c78559cd6c3006cde"), hLicense)

	hash, err := w.Commit("commit 1\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})
	s.NoError(err)
	s.Equal(plumbing.NewHash("7a7faee4630d2664a6869677cc8ab614f3fd4a18"), hash)

	infoLicense, err := fsDotgit.Stat(filepath.Join("objects", "04", "84eba0d41636ba71fa612c78559cd6c3006cde"))
	s.NoError(err) // checking objects file exists

	// Step 2: Write foo.
	time.Sleep(5 * time.Millisecond) // uncool, but we need to get different timestamps...
	util.WriteFile(fs, "foo", []byte("foo"), 0644)
	hFoo, err := w.Add("foo")
	s.NoError(err)
	s.Equal(plumbing.NewHash("19102815663d23f8b75a47e7a01965dcdc96468c"), hFoo)

	hash, err = w.Commit("commit 2\n", &CommitOptions{
		All:    true,
		Author: defaultSignature(),
	})
	s.NoError(err)
	s.Equal(plumbing.NewHash("97c0c5177e6ac57d10e8ea0017f2d39b91e2b364"), hash)

	// Step 3: Check
	// There is no need to overwrite the object of LICENSE, because its content
	// was not changed. Just a write on the object of foo is required. This behaviour
	// is fixed by #224 and tested by comparing the timestamps of the stored objects.
	infoFoo, err := fsDotgit.Stat(filepath.Join("objects", "19", "102815663d23f8b75a47e7a01965dcdc96468c"))
	s.NoError(err)                                          // checking objects file exists
	s.True(infoLicense.ModTime().Before(infoFoo.ModTime())) // object of foo has another/greaterThan timestamp than LICENSE

	infoLicenseSecond, err := fsDotgit.Stat(filepath.Join("objects", "04", "84eba0d41636ba71fa612c78559cd6c3006cde"))
	s.NoError(err)

	log.Printf("comparing mod time: %v == %v on %v (%v)", infoLicenseSecond.ModTime(), infoLicense.ModTime(), runtime.GOOS, runtime.GOARCH)
	s.Equal(infoLicense.ModTime(), infoLicenseSecond.ModTime()) // object of LICENSE should have the same timestamp because no additional write operation was performed
}

func (s *WorktreeSuite) TestCommitInvalidCharactersInAuthorInfos() {
	f := fixtures.Basic().One()
	s.Repository = NewRepositoryWithEmptyWorktree(f)

	expected := plumbing.NewHash("e8eecef2524c3a37cf0f0996603162f81e0373f1")

	fs := memfs.New()
	storage := memory.NewStorage()

	r, err := Init(storage, WithWorkTree(fs))
	s.NoError(err)

	w, err := r.Worktree()
	s.NoError(err)

	util.WriteFile(fs, "foo", []byte("foo"), 0644)

	_, err = w.Add("foo")
	s.NoError(err)

	hash, err := w.Commit("foo\n", &CommitOptions{Author: invalidSignature()})
	s.Equal(expected, hash)
	s.NoError(err)

	assertStorageStatus(s, r, 1, 1, 1, expected)

	// Check HEAD commit contains author informations with '<', '>' and '\n' stripped
	lr, err := r.Log(&LogOptions{})
	s.NoError(err)

	commit, err := lr.Next()
	s.NoError(err)

	s.Equal("foo bad", commit.Author.Name)
	s.Equal("badfoo@foo.foo", commit.Author.Email)

}

func assertStorageStatus(
	s *WorktreeSuite, r *Repository,
	treesCount, blobCount, commitCount int, head plumbing.Hash,
) {
	trees, err := r.Storer.IterEncodedObjects(plumbing.TreeObject)
	s.NoError(err)
	blobs, err := r.Storer.IterEncodedObjects(plumbing.BlobObject)
	s.NoError(err)
	commits, err := r.Storer.IterEncodedObjects(plumbing.CommitObject)
	s.NoError(err)

	s.Equal(treesCount, lenIterEncodedObjects(trees))
	s.Equal(blobCount, lenIterEncodedObjects(blobs))
	s.Equal(commitCount, lenIterEncodedObjects(commits))

	ref, err := r.Head()
	s.NoError(err)
	s.Equal(head, ref.Hash())
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

func invalidSignature() *object.Signature {
	when, _ := time.Parse(object.DateFormat, "Thu May 04 00:03:43 2017 +0200")
	return &object.Signature{
		Name:  "foo <bad>\n",
		Email: "<bad>\nfoo@foo.foo",
		When:  when,
	}
}

func commitSignKey(t *testing.T, decrypt bool) *openpgp.Entity {
	s := strings.NewReader(armoredKeyRing)
	es, err := openpgp.ReadArmoredKeyRing(s)
	assert.NoError(t, err)

	assert.Len(t, es, 1)
	assert.Len(t, es[0].Identities, 1)
	_, ok := es[0].Identities["foo bar <foo@foo.foo>"]
	assert.True(t, ok)

	key := es[0]
	if decrypt {
		err = key.PrivateKey.Decrypt([]byte(keyPassphrase))
		assert.NoError(t, err)
	}

	return key
}

const armoredKeyRing = `
-----BEGIN PGP PRIVATE KEY BLOCK-----

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
=gIM9
-----END PGP PRIVATE KEY BLOCK-----
`

const keyPassphrase = "abcdef0123456789"
