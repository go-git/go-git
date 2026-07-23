package filesystem

import (
	"crypto"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
)

type FsSuite struct {
	suite.Suite
}

var objectTypes = []plumbing.ObjectType{
	plumbing.CommitObject,
	plumbing.TagObject,
	plumbing.TreeObject,
	plumbing.BlobObject,
}

func TestFsSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(FsSuite))
}

func (s *FsSuite) TestGetFromObjectFile() {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	expected := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")
	obj, err := o.EncodedObject(s.T().Context(), plumbing.AnyObject, expected)
	s.Require().NoError(err)
	s.Equal(expected, obj.Hash())
}

func (s *FsSuite) TestGetFromPackfile() {
	for _, f := range fixtures.Basic().ByTag(".git").ByObjectFormat("sha1") {
		fs, err := f.DotGit()
		s.Require().NoError(err)
		o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

		expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
		obj, err := o.EncodedObject(s.T().Context(), plumbing.AnyObject, expected)
		s.Require().NoError(err)
		s.Equal(expected, obj.Hash())
	}
}

func (s *FsSuite) TestIterEncodedObjectsSHA256HashesRoundTrip() {
	fs, err := fixtures.ByTag(".git").ByObjectFormat("sha256").One().DotGit()
	s.Require().NoError(err)

	o := NewStorage(fs, cache.NewObjectLRUDefault())
	defer func() { _ = o.Close() }()

	iter, err := o.IterEncodedObjects(s.T().Context(), plumbing.AnyObject)
	s.Require().NoError(err)
	defer iter.Close()

	err = iter.ForEach(s.T().Context(), func(obj plumbing.EncodedObject) error {
		roundTrip, err := o.EncodedObject(s.T().Context(), plumbing.AnyObject, obj.Hash())
		s.Require().NoError(err)
		s.Equal(obj.Hash(), roundTrip.Hash())
		return nil
	})
	s.Require().NoError(err)
}

func (s *FsSuite) TestSetEncodedObjectSHA256LooseObjectRoundTrip() {
	fs := osfs.New(s.T().TempDir())
	o := NewStorageWithOptions(
		fs,
		cache.NewObjectLRUDefault(),
		Options{ObjectFormat: formatcfg.SHA256},
	)
	s.Require().NoError(o.Init(s.T().Context()))
	defer func() { _ = o.Close() }()

	obj := o.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)

	content := []byte("hello sha256\n")
	writer, err := obj.Writer()
	s.Require().NoError(err)
	_, err = writer.Write(content)
	s.Require().NoError(err)
	s.Require().NoError(writer.Close())

	hash, err := o.SetEncodedObject(s.T().Context(), obj)
	s.Require().NoError(err)
	s.Equal("2928cdcdc8b78c930378ceba09ce9ca8b888fbfe1bffb2cceb42bdff9421cb52", hash.String())
	s.Require().NoError(o.HasEncodedObject(s.T().Context(), hash))
	s.Require().FileExists(filepath.Join(fs.Root(), "objects", hash.String()[:2], hash.String()[2:]))

	roundTrip, err := o.EncodedObject(s.T().Context(), plumbing.BlobObject, hash)
	s.Require().NoError(err)
	reader, err := roundTrip.Reader()
	s.Require().NoError(err)
	defer reader.Close()
	roundTripContent, err := io.ReadAll(reader)
	s.Require().NoError(err)
	s.Equal(content, roundTripContent)
}

func firstNonMatching(packfileHash string) *fixtures.Fixture {
	for _, fix := range fixtures.ByTag(".git") {
		if fix.PackfileHash != packfileHash {
			return fix
		}
	}
	return nil
}

func (s *FsSuite) TestMismatchIdxFile() {
	f := fixtures.Basic().ByTag(".git").ByObjectFormat("sha1").One()
	fs, err := f.DotGit()
	s.Require().NoError(err)
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	fix2 := firstNonMatching(f.PackfileHash)
	s.Require().NotNil(fix2)

	idx, err := fs.OpenFile(fmt.Sprintf("objects/pack/pack-%s.idx", f.PackfileHash), os.O_TRUNC|os.O_WRONLY, 0o600)
	s.Require().NoError(err)

	idx2, err := fix2.Idx()
	s.Require().NoError(err)
	_, err = io.Copy(idx, idx2)
	s.Require().NoError(err)

	err = idx.Close()
	s.Require().NoError(err)
	err = idx2.Close()
	s.Require().NoError(err)

	expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	obj, err := o.EncodedObject(s.T().Context(), plumbing.AnyObject, expected)
	s.Require().Nil(obj)
	s.ErrorContains(err, "malformed idx file: packfile mismatch: ")
}

// TestMismatchIdxFile_LooseFallback exercises the degraded-store path:
// when the only .idx fails to load (here via a packfile-mismatch
// corruption), a loose object that lives under .git/objects must
// still resolve. Pre-fix, requireIndex returned the malformed-idx
// error from HasEncodedObject / EncodedObjectSize / EncodedObject
// before the loose path ran, hiding a perfectly good object.
func (s *FsSuite) TestMismatchIdxFile_LooseFallback() {
	f := fixtures.Basic().ByTag(".git").ByObjectFormat("sha1").One()
	fs, err := f.DotGit()
	s.Require().NoError(err)
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())
	defer func() { _ = o.Close() }()

	// Plant a loose object and capture its hash before corrupting
	// the on-disk idx so the populateIndex call below fails for
	// reasons unrelated to this object.
	obj := o.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	s.Require().NoError(err)
	_, err = w.Write([]byte("loose object behind a broken idx\n"))
	s.Require().NoError(err)
	s.Require().NoError(w.Close())
	loose, err := o.SetEncodedObject(s.T().Context(), obj)
	s.Require().NoError(err)

	// Corrupt the only idx in the same way TestMismatchIdxFile does.
	fix2 := firstNonMatching(f.PackfileHash)
	s.Require().NotNil(fix2)
	idx, err := fs.OpenFile(fmt.Sprintf("objects/pack/pack-%s.idx", f.PackfileHash), os.O_TRUNC|os.O_WRONLY, 0o600)
	s.Require().NoError(err)
	idx2, err := fix2.Idx()
	s.Require().NoError(err)
	_, err = io.Copy(idx, idx2)
	s.Require().NoError(err)
	s.Require().NoError(idx.Close())
	s.Require().NoError(idx2.Close())

	// Cold storage so requireIndex actually runs against the
	// corrupted idx on the first read.
	o2 := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())
	defer func() { _ = o2.Close() }()

	s.Require().NoError(o2.HasEncodedObject(s.T().Context(), loose),
		"loose object must answer existence even when the idx is broken")
	size, err := o2.EncodedObjectSize(s.T().Context(), loose)
	s.Require().NoError(err)
	s.Greater(size, int64(0))
	got, err := o2.EncodedObject(s.T().Context(), plumbing.AnyObject, loose)
	s.Require().NoError(err)
	s.Equal(loose, got.Hash())

	// Sanity: a hash not present anywhere still surfaces the
	// idx error so genuine on-disk corruption is not silently
	// swallowed when there is no answer to give the caller.
	missing := plumbing.NewHash("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	err = o2.HasEncodedObject(s.T().Context(), missing)
	s.ErrorContains(err, "malformed idx file: packfile mismatch: ")
}

func (s *FsSuite) TestGetSizeOfObjectFile() {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	// Get the size of `tree_walker.go`.
	expected := plumbing.NewHash("cbd81c47be12341eb1185b379d1c82675aeded6a")
	size, err := o.EncodedObjectSize(s.T().Context(), expected)
	s.Require().NoError(err)
	s.Equal(int64(2412), size)
}

func (s *FsSuite) TestGetSizeFromPackfile() {
	for _, f := range fixtures.Basic().ByTag(".git").ByObjectFormat("sha1") {
		fs, err := f.DotGit()
		s.Require().NoError(err)
		o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

		// Get the size of `binary.jpg`.
		expected := plumbing.NewHash("d5c0f4ab811897cadf03aec358ae60d21f91c50d")
		size, err := o.EncodedObjectSize(s.T().Context(), expected)
		s.Require().NoError(err)
		s.Equal(int64(76110), size)
	}
}

func (s *FsSuite) TestGetSizeOfAllObjectFiles() {
	fs, err := fixtures.ByTag(".git").One().DotGit()
	s.Require().NoError(err)
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	// Get the size of `tree_walker.go`.
	err = o.ForEachObjectHash(s.T().Context(), func(h plumbing.Hash) error {
		size, err := o.EncodedObjectSize(s.T().Context(), h)
		s.Require().NoError(err)
		s.NotEqual(int64(0), size)
		return nil
	})
	s.Require().NoError(err)
}

func (s *FsSuite) TestGetFromPackfileMultiplePackfiles() {
	fs, err := fixtures.ByTag(".git").ByTag("multi-packfile").One().DotGit()
	s.Require().NoError(err)
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	expected := plumbing.NewHash("8d45a34641d73851e01d3754320b33bb5be3c4d3")
	obj, err := o.getFromPackfile(expected, false)
	s.Require().NoError(err)
	s.Equal(expected, obj.Hash())

	expected = plumbing.NewHash("e9cfa4c9ca160546efd7e8582ec77952a27b17db")
	obj, err = o.getFromPackfile(expected, false)
	s.Require().NoError(err)
	s.Equal(expected, obj.Hash())
}

func (s *FsSuite) TestGetFromPackfileMultiplePackfilesLargeObjectThreshold() {
	fs, err := fixtures.ByTag(".git").ByTag("multi-packfile").One().DotGit()
	s.Require().NoError(err)
	o := NewObjectStorageWithOptions(dotgit.New(fs), cache.NewObjectLRUDefault(), Options{LargeObjectThreshold: 1})

	expected := plumbing.NewHash("8d45a34641d73851e01d3754320b33bb5be3c4d3")
	obj, err := o.getFromPackfile(expected, false)
	s.Require().NoError(err)
	s.Equal(expected, obj.Hash())

	expected = plumbing.NewHash("e9cfa4c9ca160546efd7e8582ec77952a27b17db")
	obj, err = o.getFromPackfile(expected, false)
	s.Require().NoError(err)
	s.Equal(expected, obj.Hash())
}

func (s *FsSuite) TestIter() {
	for _, f := range fixtures.ByTag(".git").ByTag("packfile").ByObjectFormat("sha1") {
		fs, err := f.DotGit()
		s.Require().NoError(err)
		o := NewStorage(fs, cache.NewObjectLRUDefault())
		defer func() { _ = o.Close() }()

		iter, err := o.IterEncodedObjects(s.T().Context(), plumbing.AnyObject)
		s.Require().NoError(err)

		var count int32
		err = iter.ForEach(s.T().Context(), func(_ plumbing.EncodedObject) error {
			count++
			return nil
		})

		s.Require().NoError(err)
		s.Equal(f.ObjectsCount, count)
	}
}

func (s *FsSuite) TestIterLargeObjectThreshold() {
	for _, f := range fixtures.ByTag(".git").ByTag("packfile").ByObjectFormat("sha1") {
		fs, err := f.DotGit()
		s.Require().NoError(err)
		o := NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), Options{LargeObjectThreshold: 1})
		defer func() { _ = o.Close() }()

		iter, err := o.IterEncodedObjects(s.T().Context(), plumbing.AnyObject)
		s.Require().NoError(err)

		var count int32
		err = iter.ForEach(s.T().Context(), func(_ plumbing.EncodedObject) error {
			count++
			return nil
		})

		s.Require().NoError(err)
		s.Equal(f.ObjectsCount, count)
	}
}

func (s *FsSuite) TestIterWithType() {
	for _, f := range fixtures.ByTag(".git") {
		for _, t := range objectTypes {
			fs, err := f.DotGit()
			s.Require().NoError(err)
			o := NewStorage(fs, cache.NewObjectLRUDefault())
			s.T().Cleanup(func() { _ = o.Close() })

			iter, err := o.IterEncodedObjects(s.T().Context(), t)
			s.Require().NoError(err)

			err = iter.ForEach(s.T().Context(), func(obj plumbing.EncodedObject) error {
				s.Equal(t, obj.Type())
				return nil
			})

			s.Require().NoError(err)
		}
	}
}

func (s *FsSuite) TestPackfileIter() {
	for _, f := range fixtures.ByTag(".git") {
		fs, err := f.DotGit()
		s.Require().NoError(err)
		dg := dotgit.New(fs)
		objectIDSize := objectIDSizeFromFormat(f.ObjectFormat)

		for _, t := range objectTypes {
			ph, err := dg.ObjectPacks()
			s.Require().NoError(err)

			for _, h := range ph {
				f, err := dg.ObjectPack(h)
				s.Require().NoError(err)

				idxf, err := dg.ObjectPackIdx(h)
				s.Require().NoError(err)

				iter, err := NewPackfileIter(fs, f, idxf, t, false, 0, objectIDSize)
				s.Require().NoError(err)

				err = iter.ForEach(s.T().Context(), func(o plumbing.EncodedObject) error {
					s.Equal(t, o.Type())
					return nil
				})
				s.Require().NoError(err)
			}
		}
	}
}

func objectIDSizeFromFormat(format string) int {
	if format == "sha256" {
		return crypto.SHA256.Size()
	}
	return crypto.SHA1.Size()
}

func copyFile(fs billy.Filesystem, dstFilename string, srcFile billy.File) error {
	if _, err := srcFile.Seek(0, 0); err != nil {
		return err
	}

	if err := fs.MkdirAll(filepath.Dir(dstFilename), 0o750|os.ModeDir); err != nil {
		return err
	}

	dst, err := fs.OpenFile(dstFilename, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, srcFile)
	return err
}

// TestPackfileReindex tests that externally-added packfiles are considered by go-git
// after calling the Reindex method
func (s *FsSuite) TestPackfileReindex() {
	// obtain a standalone packfile that is not part of any other repository
	// in the fixtures:
	packFixture := fixtures.ByTag("packfile").ByTag("standalone").One()
	packFile, err := packFixture.Packfile()
	s.Require().NoError(err)
	idxFile, err := packFixture.Idx()
	s.Require().NoError(err)
	packFilename := packFixture.PackfileHash
	testObjectHash := plumbing.NewHash("a771b1e94141480861332fd0e4684d33071306c6") // this is an object we know exists in the standalone packfile
	for _, f := range fixtures.ByTag(".git") {
		fs, err := f.DotGit()
		s.Require().NoError(err)
		storer := NewStorage(fs, cache.NewObjectLRUDefault())
		defer func() { _ = storer.Close() }()

		// check that our test object is NOT found
		_, err = storer.EncodedObject(s.T().Context(), plumbing.CommitObject, testObjectHash)
		s.ErrorIs(err, plumbing.ErrObjectNotFound)

		// add the external packfile+idx to the packs folder
		// this simulates a git bundle unbundle command, or a repack, for example.
		s.Require().NoError(copyFile(fs, filepath.Join("objects", "pack", fmt.Sprintf("pack-%s.pack", packFilename)), packFile))
		s.Require().NoError(copyFile(fs, filepath.Join("objects", "pack", fmt.Sprintf("pack-%s.idx", packFilename)), idxFile))

		// check that we cannot still retrieve the test object
		_, err = storer.EncodedObject(s.T().Context(), plumbing.CommitObject, testObjectHash)
		s.ErrorIs(err, plumbing.ErrObjectNotFound)

		storer.Reindex() // actually reindex

		// Now check that the test object can be retrieved
		_, err = storer.EncodedObject(s.T().Context(), plumbing.CommitObject, testObjectHash)
		s.Require().NoError(err)
	}
}

func (s *FsSuite) TestGetFromObjectFileSharedCache() {
	f1, err := fixtures.ByTag("worktree").One().DotGit()
	s.Require().NoError(err)
	f2, err := fixtures.ByTag("worktree").ByTag("submodule").One().DotGit()
	s.Require().NoError(err)

	ch := cache.NewObjectLRUDefault()
	o1 := NewObjectStorage(dotgit.New(f1), ch)
	o2 := NewObjectStorage(dotgit.New(f2), ch)

	expected := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")
	obj, err := o1.EncodedObject(s.T().Context(), plumbing.CommitObject, expected)
	s.Require().NoError(err)
	s.Equal(expected, obj.Hash())

	_, err = o2.EncodedObject(s.T().Context(), plumbing.CommitObject, expected)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *FsSuite) TestHashesWithPrefix() {
	// Same setup as TestGetFromObjectFile.
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())
	expected := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")
	obj, err := o.EncodedObject(s.T().Context(), plumbing.AnyObject, expected)
	s.Require().NoError(err)
	s.Equal(expected, obj.Hash())

	prefix, _ := hex.DecodeString("f3dfe2")
	hashes, err := o.HashesWithPrefix(prefix)
	s.Require().NoError(err)
	s.Len(hashes, 1)
	s.Equal("f3dfe29d268303fc6e1bbce268605fc99573406e", hashes[0].String())
}

func (s *FsSuite) TestHashesWithPrefixFromPackfile() {
	// Same setup as TestGetFromPackfile
	for _, f := range fixtures.Basic().ByTag(".git").ByObjectFormat("sha1") {
		fs, err := f.DotGit()
		s.Require().NoError(err)
		o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

		expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
		// Only pass the first 8 bytes
		hashes, err := o.HashesWithPrefix(expected.Bytes()[:8])
		s.Require().NoError(err)
		s.Len(hashes, 1)
		s.Equal(expected, hashes[0])
	}
}

func BenchmarkPackfileIter(b *testing.B) {
	for _, f := range fixtures.ByTag(".git") {
		b.Run(f.URL, func(b *testing.B) {
			fs, err := f.DotGit()
			if err != nil {
				b.Fatal(err)
			}
			dg := dotgit.New(fs)

			for i := 0; i < b.N; i++ {
				for _, t := range objectTypes {
					ph, err := dg.ObjectPacks()
					if err != nil {
						b.Fatal(err)
					}

					for _, h := range ph {
						f, err := dg.ObjectPack(h)
						if err != nil {
							b.Fatal(err)
						}

						idxf, err := dg.ObjectPackIdx(h)
						if err != nil {
							b.Fatal(err)
						}

						iter, err := NewPackfileIter(fs, f, idxf, t, false, 0, crypto.SHA1.Size())
						if err != nil {
							b.Fatal(err)
						}

						err = iter.ForEach(b.Context(), func(o plumbing.EncodedObject) error {
							if o.Type() != t {
								b.Errorf("expecting %s, got %s", t, o.Type())
							}
							return nil
						})
						if err != nil {
							b.Fatal(err)
						}
					}
				}
			}
		})
	}
}

func BenchmarkPackfileIterReadContent(b *testing.B) {
	for _, f := range fixtures.ByTag(".git") {
		b.Run(f.URL, func(b *testing.B) {
			fs, err := f.DotGit()
			if err != nil {
				b.Fatal(err)
			}
			dg := dotgit.New(fs)

			for i := 0; i < b.N; i++ {
				for _, t := range objectTypes {
					ph, err := dg.ObjectPacks()
					if err != nil {
						b.Fatal(err)
					}

					for _, h := range ph {
						f, err := dg.ObjectPack(h)
						if err != nil {
							b.Fatal(err)
						}

						idxf, err := dg.ObjectPackIdx(h)
						if err != nil {
							b.Fatal(err)
						}

						iter, err := NewPackfileIter(fs, f, idxf, t, false, 0, crypto.SHA1.Size())
						if err != nil {
							b.Fatal(err)
						}

						err = iter.ForEach(b.Context(), func(o plumbing.EncodedObject) error {
							if o.Type() != t {
								b.Errorf("expecting %s, got %s", t, o.Type())
							}

							r, err := o.Reader()
							if err != nil {
								b.Fatal(err)
							}

							if _, err := io.ReadAll(r); err != nil {
								b.Fatal(err)
							}

							return r.Close()
						})
						if err != nil {
							b.Fatal(err)
						}
					}
				}
			}
		})
	}
}

func BenchmarkGetObjectFromPackfile(b *testing.B) {
	for _, f := range fixtures.Basic() {
		b.Run(f.URL, func(b *testing.B) {
			fs, err := f.DotGit()
			if err != nil {
				b.Fatal(err)
			}
			o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())
			for i := 0; i < b.N; i++ {
				expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
				obj, err := o.EncodedObject(b.Context(), plumbing.AnyObject, expected)
				if err != nil {
					b.Fatal(err)
				}

				if obj.Hash() != expected {
					b.Errorf("expecting %s, got %s", expected, obj.Hash())
				}
			}
		})
	}
}

func (s *FsSuite) TestGetFromUnpackedCachesObjects() {
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	objectCache := cache.NewObjectLRUDefault()
	objectStorage := NewObjectStorage(dotgit.New(fs), objectCache)
	hash := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")

	// Assert the cache is empty initially
	_, ok := objectCache.Get(hash)
	s.False(ok)

	// Load the object
	obj, err := objectStorage.EncodedObject(s.T().Context(), plumbing.AnyObject, hash)
	s.Require().NoError(err)
	s.Equal(hash, obj.Hash())

	// The object should've been cached during the load
	cachedObj, ok := objectCache.Get(hash)
	s.True(ok)
	s.Equal(obj, cachedObj)

	// Assert that both objects can be read and that they both produce the same bytes

	objReader, err := obj.Reader()
	s.Require().NoError(err)
	objBytes, err := io.ReadAll(objReader)
	s.Require().NoError(err)
	s.NotEqual(0, len(objBytes))
	err = objReader.Close()
	s.Require().NoError(err)

	cachedObjReader, err := cachedObj.Reader()
	s.Require().NoError(err)
	cachedObjBytes, err := io.ReadAll(cachedObjReader)
	s.NotEqual(0, len(cachedObjBytes))
	s.Require().NoError(err)
	err = cachedObjReader.Close()
	s.Require().NoError(err)

	s.Equal(objBytes, cachedObjBytes)
}

func (s *FsSuite) TestGetFromUnpackedDoesNotCacheLargeObjects() {
	// The "unpacked" fixture contains both packed and loose copies
	// of the same objects. EncodedObject routes packed reads
	// through the pack path (which caches independently of size),
	// so to test the LargeObjectThreshold behaviour we exercise
	// getFromUnpacked directly.
	fs, err := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	s.Require().NoError(err)
	objectCache := cache.NewObjectLRUDefault()
	objectStorage := NewObjectStorageWithOptions(dotgit.New(fs), objectCache, Options{LargeObjectThreshold: 1})
	hash := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")

	// Assert the cache is empty initially
	_, ok := objectCache.Get(hash)
	s.False(ok)

	// Load the object via the loose path.
	obj, err := objectStorage.getFromUnpacked(hash)
	s.Require().NoError(err)
	s.Equal(hash, obj.Hash())

	// The object should not have been cached during the load
	_, ok = objectCache.Get(hash)
	s.False(ok)
}

// TestObjectStorageMultipleAlternates verifies that objects can be found
// across multiple alternate repositories.
func (s *FsSuite) TestObjectStorageMultipleAlternates() {
	baseDir := s.T().TempDir()

	templateFs1, err := fixtures.Basic().ByTag(".git").One().DotGit(fixtures.WithTargetDir(func() string { return baseDir }))
	s.Require().NoError(err)
	templateFs2, err := fixtures.ByURL("https://github.com/git-fixtures/submodule.git").One().DotGit(fixtures.WithTargetDir(func() string { return baseDir }))
	s.Require().NoError(err)

	commitHash1 := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	commitHash2 := plumbing.NewHash("b685400c1f9316f350965a5993d350bc746b0bf4")

	workDotGit := filepath.Join(baseDir, "work", ".git")
	alternatesDir := filepath.Join(workDotGit, "objects", "info")
	err = os.MkdirAll(alternatesDir, 0o755)
	s.Require().NoError(err)
	alternatesContent := templateFs1.Root() + "/objects\n" + templateFs2.Root() + "/objects\n"
	alternatesFile := filepath.Join(alternatesDir, "alternates")
	err = os.WriteFile(alternatesFile, []byte(alternatesContent), 0o644)
	s.Require().NoError(err)

	rootFs := osfs.New(baseDir)
	workFs, err := rootFs.Chroot(filepath.Join("work", ".git"))
	s.Require().NoError(err)
	dg := dotgit.NewWithOptions(workFs, dotgit.Options{AlternatesFS: rootFs})
	storage := NewObjectStorage(dg, cache.NewObjectLRUDefault())

	obj1, err := storage.EncodedObject(s.T().Context(), plumbing.AnyObject, commitHash1)
	s.Require().NoError(err)
	s.Equal(commitHash1, obj1.Hash())

	obj2, err := storage.EncodedObject(s.T().Context(), plumbing.AnyObject, commitHash2)
	s.Require().NoError(err)
	s.Equal(commitHash2, obj2.Hash())

	err = storage.Close()
	s.Require().NoError(err)
}

// TestObjectStorageAlternatesHasEncodedObject verifies HasEncodedObject
// correctly checks alternates.
func (s *FsSuite) TestObjectStorageAlternatesHasEncodedObject() {
	baseDir := s.T().TempDir()
	templateFs, err := fixtures.Basic().ByTag(".git").One().DotGit(fixtures.WithTargetDir(func() string { return baseDir }))
	s.Require().NoError(err)
	commitHash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
	nonExistentHash := plumbing.NewHash("0000000000000000000000000000000000000000")

	workDotGit := filepath.Join(baseDir, "work", ".git")
	alternatesDir := filepath.Join(workDotGit, "objects", "info")
	err = os.MkdirAll(alternatesDir, 0o755)
	s.Require().NoError(err)
	alternatesFile := filepath.Join(alternatesDir, "alternates")
	err = os.WriteFile(alternatesFile, []byte(templateFs.Root()+"/objects\n"), 0o644)
	s.Require().NoError(err)

	rootFs := osfs.New(baseDir)
	workFs, err := rootFs.Chroot(filepath.Join("work", ".git"))
	s.Require().NoError(err)
	dg := dotgit.NewWithOptions(workFs, dotgit.Options{AlternatesFS: rootFs})
	storage := NewObjectStorage(dg, cache.NewObjectLRUDefault())

	err = storage.HasEncodedObject(s.T().Context(), commitHash)
	s.NoError(err)

	err = storage.HasEncodedObject(s.T().Context(), nonExistentHash)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)

	err = storage.Close()
	s.Require().NoError(err)
}

// TestObjectStorageAlternatesHashesWithPrefix verifies HashesWithPrefix
// finds objects stored in alternates.
func (s *FsSuite) TestObjectStorageAlternatesHashesWithPrefix() {
	baseDir := s.T().TempDir()
	templateFs, err := fixtures.Basic().ByTag(".git").One().DotGit(fixtures.WithTargetDir(func() string { return baseDir }))
	s.Require().NoError(err)
	commitHash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	workDotGit := filepath.Join(baseDir, "work", ".git")
	alternatesDir := filepath.Join(workDotGit, "objects", "info")
	err = os.MkdirAll(alternatesDir, 0o755)
	s.Require().NoError(err)
	alternatesFile := filepath.Join(alternatesDir, "alternates")
	err = os.WriteFile(alternatesFile, []byte(templateFs.Root()+"/objects\n"), 0o644)
	s.Require().NoError(err)

	rootFs := osfs.New(baseDir)
	workFs, err := rootFs.Chroot(filepath.Join("work", ".git"))
	s.Require().NoError(err)
	dg := dotgit.NewWithOptions(workFs, dotgit.Options{AlternatesFS: rootFs})
	storage := NewObjectStorage(dg, cache.NewObjectLRUDefault())

	hashes, err := storage.HashesWithPrefix(commitHash.Bytes()[:4])
	s.Require().NoError(err)
	s.Len(hashes, 1)
	s.Equal(commitHash, hashes[0])

	err = storage.Close()
	s.Require().NoError(err)
}

// TestObjectStorageAlternatesEncodedObjectSize verifies EncodedObjectSize
// correctly checks alternates.
func (s *FsSuite) TestObjectStorageAlternatesEncodedObjectSize() {
	baseDir := s.T().TempDir()
	templateFs, err := fixtures.Basic().ByTag(".git").One().DotGit(fixtures.WithTargetDir(func() string { return baseDir }))
	s.Require().NoError(err)
	commitHash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	workDotGit := filepath.Join(baseDir, "work", ".git")
	alternatesDir := filepath.Join(workDotGit, "objects", "info")
	err = os.MkdirAll(alternatesDir, 0o755)
	s.Require().NoError(err)
	alternatesFile := filepath.Join(alternatesDir, "alternates")
	err = os.WriteFile(alternatesFile, []byte(templateFs.Root()+"/objects\n"), 0o644)
	s.Require().NoError(err)

	rootFs := osfs.New(baseDir)
	workFs, err := rootFs.Chroot(filepath.Join("work", ".git"))
	s.Require().NoError(err)
	dg := dotgit.NewWithOptions(workFs, dotgit.Options{AlternatesFS: rootFs})
	storage := NewObjectStorage(dg, cache.NewObjectLRUDefault())

	size, err := storage.EncodedObjectSize(s.T().Context(), commitHash)
	s.NoError(err)
	s.Greater(size, int64(0))

	err = storage.Close()
	s.Require().NoError(err)
}

// TestObjectStorageAlternatesReset verifies that AddAlternate invalidates
// the cached alternate state so that subsequent lookups pick up new alternates.
func (s *FsSuite) TestObjectStorageAlternatesReset() {
	baseDir := s.T().TempDir()
	templateFs, err := fixtures.Basic().ByTag(".git").One().DotGit(fixtures.WithTargetDir(func() string { return baseDir }))
	s.Require().NoError(err)
	commitHash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	rootFs := osfs.New(baseDir)
	workFs, err := rootFs.Chroot(filepath.Join("work", ".git"))
	s.Require().NoError(err)

	storage := NewStorageWithOptions(workFs, cache.NewObjectLRUDefault(), Options{AlternatesFS: rootFs})
	s.T().Cleanup(func() { storage.Close() })
	s.Require().NoError(storage.Init(s.T().Context()))

	err = storage.HasEncodedObject(s.T().Context(), commitHash)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)

	err = storage.AddAlternate(s.T().Context(), templateFs.Root())
	s.Require().NoError(err)

	err = storage.HasEncodedObject(s.T().Context(), commitHash)
	s.NoError(err)

	obj, err := storage.EncodedObject(s.T().Context(), plumbing.AnyObject, commitHash)
	s.NoError(err)
	s.Equal(commitHash, obj.Hash())
}

// TestObjectStorageAlternatesInitError verifies that non-os.ErrNotExist errors
// from reading alternates are propagated to callers.
func (s *FsSuite) TestObjectStorageAlternatesInitError() {
	baseDir := s.T().TempDir()
	commitHash := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	workDotGit := filepath.Join(baseDir, "work", ".git")
	alternatesDir := filepath.Join(workDotGit, "objects", "info")
	err := os.MkdirAll(alternatesDir, 0o755)
	s.Require().NoError(err)

	// Point the alternate at a regular file instead of a directory to trigger
	// an "invalid object directory" error from DotGit.Alternates().
	badTarget := filepath.Join(baseDir, "not-a-directory")
	err = os.WriteFile(badTarget, []byte("placeholder"), 0o644)
	s.Require().NoError(err)

	alternatesFile := filepath.Join(alternatesDir, "alternates")
	err = os.WriteFile(alternatesFile, []byte(badTarget+"\n"), 0o644)
	s.Require().NoError(err)

	rootFs := osfs.New(baseDir)
	workFs, err := rootFs.Chroot(filepath.Join("work", ".git"))
	s.Require().NoError(err)
	dg := dotgit.NewWithOptions(workFs, dotgit.Options{AlternatesFS: rootFs})
	storage := NewObjectStorage(dg, cache.NewObjectLRUDefault())
	s.T().Cleanup(func() { storage.Close() })

	err = storage.HasEncodedObject(s.T().Context(), commitHash)
	s.Error(err)
	s.NotErrorIs(err, plumbing.ErrObjectNotFound)

	_, err = storage.EncodedObjectSize(s.T().Context(), commitHash)
	s.Error(err)
	s.NotErrorIs(err, plumbing.ErrObjectNotFound)

	_, err = storage.EncodedObject(s.T().Context(), plumbing.AnyObject, commitHash)
	s.Error(err)
	s.NotErrorIs(err, plumbing.ErrObjectNotFound)
}

// TestObjectStorageCloseIdleDescriptors groups the cases for the
// ObjectStorage-layer soft-close.
func TestObjectStorageCloseIdleDescriptors(t *testing.T) {
	t.Parallel()

	t.Run("DropsPackFDsButPreservesReadability", func(t *testing.T) {
		t.Parallel()
		fixture := fixtures.Basic().One()
		dir, err := fixture.DotGit()
		require.NoError(t, err)
		s := NewStorage(dir, cache.NewObjectLRUDefault())
		t.Cleanup(func() { _ = s.Close() })

		// Get one hash to read.
		iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
		require.NoError(t, err)
		obj1, err := iter.Next(t.Context())
		require.NoError(t, err)
		iter.Close()

		// Read once to warm caches.
		_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj1.Hash())
		require.NoError(t, err)

		require.NoError(t, s.CloseIdleDescriptors())

		// Second read still works.
		obj2, err := s.EncodedObject(t.Context(), plumbing.AnyObject, obj1.Hash())
		require.NoError(t, err)
		assert.Equal(t, obj1.Hash(), obj2.Hash())
	})

	t.Run("PreservesIndexMap", func(t *testing.T) {
		t.Parallel()
		fixture := fixtures.Basic().One()
		dir, err := fixture.DotGit()
		require.NoError(t, err)
		s := NewStorage(dir, cache.NewObjectLRUDefault())
		t.Cleanup(func() { _ = s.Close() })

		// Force population of s.index.
		iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
		require.NoError(t, err)
		obj1, err := iter.Next(t.Context())
		require.NoError(t, err)
		iter.Close()

		_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj1.Hash())
		require.NoError(t, err)

		s.muI.RLock()
		idxBefore := make(map[plumbing.Hash]struct{}, len(s.index))
		for h := range s.index {
			idxBefore[h] = struct{}{}
		}
		s.muI.RUnlock()

		require.NoError(t, s.CloseIdleDescriptors())

		s.muI.RLock()
		assert.Equal(t, len(idxBefore), len(s.index))
		for h := range idxBefore {
			_, ok := s.index[h]
			assert.True(t, ok, "index entry %s should survive", h)
		}
		s.muI.RUnlock()
	})

	t.Run("TwiceInARow", func(t *testing.T) {
		t.Parallel()
		fixture := fixtures.Basic().One()
		dir, err := fixture.DotGit()
		require.NoError(t, err)
		s := NewStorage(dir, cache.NewObjectLRUDefault())
		t.Cleanup(func() { _ = s.Close() })

		require.NoError(t, s.CloseIdleDescriptors())
		require.NoError(t, s.CloseIdleDescriptors())
	})

	t.Run("AfterClose_NoOp", func(t *testing.T) {
		t.Parallel()
		fixture := fixtures.Basic().One()
		dir, err := fixture.DotGit()
		require.NoError(t, err)
		s := NewStorage(dir, cache.NewObjectLRUDefault())
		require.NoError(t, s.Close())
		require.NoError(t, s.CloseIdleDescriptors())
		require.NoError(t, s.CloseIdleDescriptors())
	})

	t.Run("ConcurrentWithReads", func(t *testing.T) {
		t.Parallel()
		fixture := fixtures.Basic().One()
		dir, err := fixture.DotGit()
		require.NoError(t, err)
		s := NewStorage(dir, cache.NewObjectLRUDefault())
		t.Cleanup(func() { _ = s.Close() })

		// Collect a working set of hashes.
		iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
		require.NoError(t, err)
		var hashes []plumbing.Hash
		for range 16 {
			obj, err := iter.Next(t.Context())
			if err != nil {
				break
			}
			hashes = append(hashes, obj.Hash())
		}
		iter.Close()
		require.NotEmpty(t, hashes)

		// Single-goroutine warm-up: the default loose-first probe calls
		// dotgit.hasIncomingObjects(), which lazily initialises
		// incomingChecked without a lock. One serial lookup here
		// ensures the field is set before the concurrent herd,
		// sidestepping the pre-existing race in hasIncomingObjects.
		_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, hashes[0])
		require.NoError(t, err)

		var wg sync.WaitGroup
		var failures atomic.Int32

		for range 8 {
			wg.Go(func() {
				for _, h := range hashes {
					obj, err := s.EncodedObject(t.Context(), plumbing.AnyObject, h)
					if err != nil || obj == nil {
						failures.Add(1)
						// Continue rather than return so a single
						// transient failure does not mask later
						// reads from this goroutine.
						continue
					}
				}
			})
		}
		for range 4 {
			wg.Go(func() {
				for range 16 {
					if err := s.CloseIdleDescriptors(); err != nil {
						failures.Add(1)
					}
				}
			})
		}
		wg.Wait()
		assert.Equal(t, int32(0), failures.Load(),
			"no read or release should fail under concurrent load")
	})
}

// TestObjectStorage_PopulateIndex_FirstReadHotMap asserts that the
// first observable read leaves s.index populated, i.e. requireIndex
// publishes the local map into the shared field under the lock.
func TestObjectStorage_PopulateIndex_FirstReadHotMap(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	obj, err := iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()

	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj.Hash())
	require.NoError(t, err)

	s.muI.RLock()
	defer s.muI.RUnlock()
	assert.NotNil(t, s.index, "first read should publish s.index")
	assert.NotEmpty(t, s.index, "fixture must contribute at least one pack")
}

// TestObjectStorage_ReindexPrewarms verifies that Reindex leaves
// s.index in a hot, non-nil state so the next read does not pay a
// cold-load. The pre-Phase-3 implementation niled s.index and
// deferred reload to the next read.
func TestObjectStorage_ReindexPrewarms(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	obj, err := iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()

	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj.Hash())
	require.NoError(t, err)

	require.NoError(t, s.Reindex())

	// Reindex must synchronously re-populate s.index.
	s.muI.RLock()
	idxPopulated := len(s.index) > 0
	s.muI.RUnlock()
	assert.True(t, idxPopulated,
		"Reindex should leave s.index pre-warmed for the next read")

	// And the next read should still succeed.
	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj.Hash())
	require.NoError(t, err)
}

// TestObjectStorage_ConcurrentReindex_NoRace runs many concurrent
// Reindex calls against the same Storage and asserts that the
// singleflight wrapper keeps the operation safe: every call returns
// nil, s.index stays non-empty and consistent throughout, and reads
// after the herd still succeed. Without singleflight the goroutines
// would each populate independently and race on the map swap,
// producing duplicate disk I/O and a window where a reader could
// observe a partially-published map.
func TestObjectStorage_ConcurrentReindex_NoRace(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	// Pre-warm so a concurrent reader can RLock muI without first
	// racing requireIndex against the Reindex herd.
	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	obj, err := iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()
	h := obj.Hash()

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			assert.NoError(t, s.Reindex())
		})
	}
	// A few readers in parallel exercise the muI.RLock fast-path
	// during the Reindex herd.
	for range 8 {
		wg.Go(func() {
			_, err := s.EncodedObject(t.Context(), plumbing.AnyObject, h)
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	s.muI.RLock()
	assert.NotEmpty(t, s.index, "after the Reindex herd s.index must be populated")
	s.muI.RUnlock()

	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, h)
	require.NoError(t, err, "reads after the Reindex herd must still succeed")
}

// TestObjectStorage_ConcurrentEncodedObject_NoRace slams many
// goroutines at the same storage and verifies the read path is
// race-free under -race. The map is pre-warmed by IterEncodedObjects
// before the herd, so the herd exercises the RLock fast-path of
// requireIndex (singleflight coalescing is exercised separately by
// the cold-load benchmark).
func TestObjectStorage_ConcurrentEncodedObject_NoRace(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	obj, err := iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()
	h := obj.Hash()

	// Single-goroutine warm-up: the default loose-first probe calls
	// dotgit.hasIncomingObjects(), which lazily writes incomingChecked
	// without a lock. One serial call here ensures the field is set
	// before the concurrent herd, avoiding a pre-existing race in
	// hasIncomingObjects on cold startup.
	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, h)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			_, err := s.EncodedObject(t.Context(), plumbing.AnyObject, h)
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	s.muI.RLock()
	defer s.muI.RUnlock()
	assert.NotNil(t, s.index, "after the herd s.index must be populated")
}

func TestEncodedObject_PackedRoundTrip(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	// Warm the index so EncodedObject doesn't race through the
	// cold dotgit path (matches the convention of existing tests).
	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	obj, err := iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()

	got, err := s.EncodedObject(t.Context(), plumbing.AnyObject, obj.Hash())
	require.NoError(t, err)
	assert.Equal(t, obj.Hash(), got.Hash())
}

func TestEncodedObject_LooseRoundTrip(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	// Write a fresh loose object; pack-membership-first must route
	// the read through the loose path because the new blob isn't
	// in any pack.
	o := s.NewEncodedObject()
	o.SetType(plumbing.BlobObject)
	w, err := o.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte("loose-prefer-test"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	h, err := s.SetEncodedObject(t.Context(), o)
	require.NoError(t, err)

	got, err := s.EncodedObject(t.Context(), plumbing.AnyObject, h)
	require.NoError(t, err)
	assert.Equal(t, h, got.Hash())
}

func TestEncodedObject_NotFound(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	// Warm index to avoid the cold dotgit race.
	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	iter.Close()

	missing := plumbing.NewHash("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, missing)
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
}

func TestEncodedObjectSize_NotFound(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	// Warm index to avoid the cold dotgit race.
	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	iter.Close()

	missing := plumbing.NewHash("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	_, err = s.EncodedObjectSize(t.Context(), missing)
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
}

func TestEncodedObjectSize_Packed(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	obj, err := iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()

	size, err := s.EncodedObjectSize(t.Context(), obj.Hash())
	require.NoError(t, err)
	assert.Greater(t, size, int64(0))
}

func TestFindObjectInPackfile_MRU_SeedsAndUses(t *testing.T) {
	t.Parallel()
	fixture := fixtures.Basic().One()
	dir, err := fixture.DotGit()
	require.NoError(t, err)
	s := NewStorage(dir, cache.NewObjectLRUDefault())
	t.Cleanup(func() { _ = s.Close() })

	// Pre-warm the index (cold dotgit race avoidance).
	iter, err := s.IterEncodedObjects(t.Context(), plumbing.AnyObject)
	require.NoError(t, err)
	obj, err := iter.Next(t.Context())
	require.NoError(t, err)
	iter.Close()

	// Initially no hint (encoded as 0).
	require.Equal(t, int32(0), s.lastHitPackIdx.Load())

	// First lookup seeds the hint.
	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj.Hash())
	require.NoError(t, err)

	hint := s.lastHitPackIdx.Load()
	require.NotEqual(t, int32(0), hint,
		"first successful lookup should seed lastHitPackIdx")

	// Second lookup should not change the hint (still the same pack).
	_, err = s.EncodedObject(t.Context(), plumbing.AnyObject, obj.Hash())
	require.NoError(t, err)
	assert.Equal(t, hint, s.lastHitPackIdx.Load(),
		"single-pack fixture: hint should stay stable")
}
