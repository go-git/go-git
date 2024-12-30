package filesystem

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
	"github.com/stretchr/testify/suite"

	fixtures "github.com/go-git/go-git-fixtures/v4"
)

type FsFixtureSuite struct {
	fixtures.Suite
}

type FsSuite struct {
	suite.Suite
	FsFixtureSuite
}

var objectTypes = []plumbing.ObjectType{
	plumbing.CommitObject,
	plumbing.TagObject,
	plumbing.TreeObject,
	plumbing.BlobObject,
}

func TestFsSuite(t *testing.T) {
	suite.Run(t, new(FsSuite))
}

func (s *FsSuite) TestGetFromObjectFile() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	expected := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")
	obj, err := o.EncodedObject(plumbing.AnyObject, expected)
	s.NoError(err)
	s.Equal(expected, obj.Hash())
}

func (s *FsSuite) TestGetFromPackfile() {
	for _, f := range fixtures.Basic().ByTag(".git") {
		fs := f.DotGit()
		o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

		expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
		obj, err := o.EncodedObject(plumbing.AnyObject, expected)
		s.NoError(err)
		s.Equal(expected, obj.Hash())
	}
}

func (s *FsSuite) TestGetFromPackfileKeepDescriptors() {
	for _, f := range fixtures.Basic().ByTag(".git") {
		fs := f.DotGit()
		dg := dotgit.NewWithOptions(fs, dotgit.Options{KeepDescriptors: true})
		o := NewObjectStorageWithOptions(dg, cache.NewObjectLRUDefault(), Options{KeepDescriptors: true})

		expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
		obj, err := o.EncodedObject(plumbing.AnyObject, expected)
		s.NoError(err)
		s.Equal(expected, obj.Hash())

		packfiles, err := dg.ObjectPacks()
		s.NoError(err)

		pack1, err := dg.ObjectPack(packfiles[0])
		s.NoError(err)

		pack1.Seek(42, io.SeekStart)

		err = o.Close()
		s.NoError(err)

		pack2, err := dg.ObjectPack(packfiles[0])
		s.NoError(err)

		offset, err := pack2.Seek(0, io.SeekCurrent)
		s.NoError(err)
		s.Equal(int64(0), offset)

		err = o.Close()
		s.NoError(err)

	}
}

func (s *FsSuite) TestGetFromPackfileMaxOpenDescriptors() {
	fs := fixtures.ByTag(".git").ByTag("multi-packfile").One().DotGit()
	o := NewObjectStorageWithOptions(dotgit.New(fs), cache.NewObjectLRUDefault(), Options{MaxOpenDescriptors: 1})

	expected := plumbing.NewHash("8d45a34641d73851e01d3754320b33bb5be3c4d3")
	obj, err := o.getFromPackfile(expected, false)
	s.NoError(err)
	s.Equal(expected, obj.Hash())

	expected = plumbing.NewHash("e9cfa4c9ca160546efd7e8582ec77952a27b17db")
	obj, err = o.getFromPackfile(expected, false)
	s.NoError(err)
	s.Equal(expected, obj.Hash())

	err = o.Close()
	s.NoError(err)
}

func (s *FsSuite) TestGetFromPackfileMaxOpenDescriptorsLargeObjectThreshold() {
	fs := fixtures.ByTag(".git").ByTag("multi-packfile").One().DotGit()
	o := NewObjectStorageWithOptions(dotgit.New(fs), cache.NewObjectLRUDefault(), Options{
		MaxOpenDescriptors:   1,
		LargeObjectThreshold: 1,
	})

	expected := plumbing.NewHash("8d45a34641d73851e01d3754320b33bb5be3c4d3")
	obj, err := o.getFromPackfile(expected, false)
	s.NoError(err)
	s.Equal(expected, obj.Hash())

	expected = plumbing.NewHash("e9cfa4c9ca160546efd7e8582ec77952a27b17db")
	obj, err = o.getFromPackfile(expected, false)
	s.NoError(err)
	s.Equal(expected, obj.Hash())

	err = o.Close()
	s.NoError(err)
}

func (s *FsSuite) TestGetSizeOfObjectFile() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	// Get the size of `tree_walker.go`.
	expected := plumbing.NewHash("cbd81c47be12341eb1185b379d1c82675aeded6a")
	size, err := o.EncodedObjectSize(expected)
	s.NoError(err)
	s.Equal(int64(2412), size)
}

func (s *FsSuite) TestGetSizeFromPackfile() {
	for _, f := range fixtures.Basic().ByTag(".git") {
		fs := f.DotGit()
		o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

		// Get the size of `binary.jpg`.
		expected := plumbing.NewHash("d5c0f4ab811897cadf03aec358ae60d21f91c50d")
		size, err := o.EncodedObjectSize(expected)
		s.NoError(err)
		s.Equal(int64(76110), size)
	}
}

func (s *FsSuite) TestGetSizeOfAllObjectFiles() {
	fs := fixtures.ByTag(".git").One().DotGit()
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	// Get the size of `tree_walker.go`.
	err := o.ForEachObjectHash(func(h plumbing.Hash) error {
		size, err := o.EncodedObjectSize(h)
		s.NoError(err)
		s.NotEqual(int64(0), size)
		return nil
	})
	s.NoError(err)
}

func (s *FsSuite) TestGetFromPackfileMultiplePackfiles() {
	fs := fixtures.ByTag(".git").ByTag("multi-packfile").One().DotGit()
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

	expected := plumbing.NewHash("8d45a34641d73851e01d3754320b33bb5be3c4d3")
	obj, err := o.getFromPackfile(expected, false)
	s.NoError(err)
	s.Equal(expected, obj.Hash())

	expected = plumbing.NewHash("e9cfa4c9ca160546efd7e8582ec77952a27b17db")
	obj, err = o.getFromPackfile(expected, false)
	s.NoError(err)
	s.Equal(expected, obj.Hash())
}

func (s *FsSuite) TestGetFromPackfileMultiplePackfilesLargeObjectThreshold() {
	fs := fixtures.ByTag(".git").ByTag("multi-packfile").One().DotGit()
	o := NewObjectStorageWithOptions(dotgit.New(fs), cache.NewObjectLRUDefault(), Options{LargeObjectThreshold: 1})

	expected := plumbing.NewHash("8d45a34641d73851e01d3754320b33bb5be3c4d3")
	obj, err := o.getFromPackfile(expected, false)
	s.NoError(err)
	s.Equal(expected, obj.Hash())

	expected = plumbing.NewHash("e9cfa4c9ca160546efd7e8582ec77952a27b17db")
	obj, err = o.getFromPackfile(expected, false)
	s.NoError(err)
	s.Equal(expected, obj.Hash())
}

func (s *FsSuite) TestIter() {
	for _, f := range fixtures.ByTag(".git").ByTag("packfile") {
		fs := f.DotGit()
		o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

		iter, err := o.IterEncodedObjects(plumbing.AnyObject)
		s.NoError(err)

		var count int32
		err = iter.ForEach(func(o plumbing.EncodedObject) error {
			count++
			return nil
		})

		s.NoError(err)
		s.Equal(f.ObjectsCount, count)
	}
}

func (s *FsSuite) TestIterLargeObjectThreshold() {
	for _, f := range fixtures.ByTag(".git").ByTag("packfile") {
		fs := f.DotGit()
		o := NewObjectStorageWithOptions(dotgit.New(fs), cache.NewObjectLRUDefault(), Options{LargeObjectThreshold: 1})

		iter, err := o.IterEncodedObjects(plumbing.AnyObject)
		s.NoError(err)

		var count int32
		err = iter.ForEach(func(o plumbing.EncodedObject) error {
			count++
			return nil
		})

		s.NoError(err)
		s.Equal(f.ObjectsCount, count)
	}
}

func (s *FsSuite) TestIterWithType() {
	for _, f := range fixtures.ByTag(".git") {
		for _, t := range objectTypes {
			fs := f.DotGit()
			o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

			iter, err := o.IterEncodedObjects(t)
			s.NoError(err)

			err = iter.ForEach(func(o plumbing.EncodedObject) error {
				s.Equal(t, o.Type())
				return nil
			})

			s.NoError(err)
		}

	}
}

func (s *FsSuite) TestPackfileIter() {
	for _, f := range fixtures.ByTag(".git") {
		fs := f.DotGit()
		dg := dotgit.New(fs)

		for _, t := range objectTypes {
			ph, err := dg.ObjectPacks()
			s.NoError(err)

			for _, h := range ph {
				f, err := dg.ObjectPack(h)
				s.NoError(err)

				idxf, err := dg.ObjectPackIdx(h)
				s.NoError(err)

				iter, err := NewPackfileIter(fs, f, idxf, t, false, 0)
				s.NoError(err)

				err = iter.ForEach(func(o plumbing.EncodedObject) error {
					s.Equal(t, o.Type())
					return nil
				})
				s.NoError(err)
			}
		}
	}
}

func copyFile(s *FsSuite, dstDir, dstFilename string, srcFile billy.File) {
	_, err := srcFile.Seek(0, 0)
	s.NoError(err)

	err = osfs.Default.MkdirAll(dstDir, 0750|os.ModeDir)
	s.NoError(err)

	dst, err := osfs.Default.OpenFile(filepath.Join(dstDir, dstFilename), os.O_CREATE|os.O_WRONLY, 0666)
	s.NoError(err)
	defer dst.Close()

	_, err = io.Copy(dst, srcFile)
	s.NoError(err)
}

// TestPackfileReindex tests that externally-added packfiles are considered by go-git
// after calling the Reindex method
func (s *FsSuite) TestPackfileReindex() {
	// obtain a standalone packfile that is not part of any other repository
	// in the fixtures:
	packFixture := fixtures.ByTag("packfile").ByTag("standalone").One()
	packFile := packFixture.Packfile()
	idxFile := packFixture.Idx()
	packFilename := packFixture.PackfileHash
	testObjectHash := plumbing.NewHash("a771b1e94141480861332fd0e4684d33071306c6") // this is an object we know exists in the standalone packfile
	for _, f := range fixtures.ByTag(".git") {
		fs := f.DotGit()
		storer := NewStorage(fs, cache.NewObjectLRUDefault())

		// check that our test object is NOT found
		_, err := storer.EncodedObject(plumbing.CommitObject, testObjectHash)
		s.ErrorIs(err, plumbing.ErrObjectNotFound)

		// add the external packfile+idx to the packs folder
		// this simulates a git bundle unbundle command, or a repack, for example.
		copyFile(s, filepath.Join(storer.Filesystem().Root(), "objects", "pack"),
			fmt.Sprintf("pack-%s.pack", packFilename), packFile)
		copyFile(s, filepath.Join(storer.Filesystem().Root(), "objects", "pack"),
			fmt.Sprintf("pack-%s.idx", packFilename), idxFile)

		// check that we cannot still retrieve the test object
		_, err = storer.EncodedObject(plumbing.CommitObject, testObjectHash)
		s.ErrorIs(err, plumbing.ErrObjectNotFound)

		storer.Reindex() // actually reindex

		// Now check that the test object can be retrieved
		_, err = storer.EncodedObject(plumbing.CommitObject, testObjectHash)
		s.NoError(err)

	}
}

func (s *FsSuite) TestPackfileIterKeepDescriptors() {
	s.T().Skip("packfileIter with keep descriptors is currently broken")

	for _, f := range fixtures.ByTag(".git") {
		fs := f.DotGit()
		ops := dotgit.Options{KeepDescriptors: true}
		dg := dotgit.NewWithOptions(fs, ops)

		for _, t := range objectTypes {
			ph, err := dg.ObjectPacks()
			s.NoError(err)

			for _, h := range ph {
				f, err := dg.ObjectPack(h)
				s.NoError(err)

				idxf, err := dg.ObjectPackIdx(h)
				s.NoError(err)

				iter, err := NewPackfileIter(fs, f, idxf, t, true, 0)
				s.NoError(err)

				if err != nil {
					continue
				}

				err = iter.ForEach(func(o plumbing.EncodedObject) error {
					s.Equal(t, o.Type())
					return nil
				})
				s.NoError(err)

				// test twice to check that packfiles are not closed
				err = iter.ForEach(func(o plumbing.EncodedObject) error {
					s.Equal(t, o.Type())
					return nil
				})
				s.NoError(err)
			}
		}
	}
}

func (s *FsSuite) TestGetFromObjectFileSharedCache() {
	f1 := fixtures.ByTag("worktree").One().DotGit()
	f2 := fixtures.ByTag("worktree").ByTag("submodule").One().DotGit()

	ch := cache.NewObjectLRUDefault()
	o1 := NewObjectStorage(dotgit.New(f1), ch)
	o2 := NewObjectStorage(dotgit.New(f2), ch)

	expected := plumbing.NewHash("af2d6a6954d532f8ffb47615169c8fdf9d383a1a")
	obj, err := o1.EncodedObject(plumbing.CommitObject, expected)
	s.NoError(err)
	s.Equal(expected, obj.Hash())

	_, err = o2.EncodedObject(plumbing.CommitObject, expected)
	s.ErrorIs(err, plumbing.ErrObjectNotFound)
}

func (s *FsSuite) TestHashesWithPrefix() {
	// Same setup as TestGetFromObjectFile.
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())
	expected := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")
	obj, err := o.EncodedObject(plumbing.AnyObject, expected)
	s.NoError(err)
	s.Equal(expected, obj.Hash())

	prefix, _ := hex.DecodeString("f3dfe2")
	hashes, err := o.HashesWithPrefix(prefix)
	s.NoError(err)
	s.Len(hashes, 1)
	s.Equal("f3dfe29d268303fc6e1bbce268605fc99573406e", hashes[0].String())
}

func (s *FsSuite) TestHashesWithPrefixFromPackfile() {
	// Same setup as TestGetFromPackfile
	for _, f := range fixtures.Basic().ByTag(".git") {
		fs := f.DotGit()
		o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())

		expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
		// Only pass the first 8 bytes
		hashes, err := o.HashesWithPrefix(expected[:8])
		s.NoError(err)
		s.Len(hashes, 1)
		s.Equal(expected, hashes[0])
	}
}

func BenchmarkPackfileIter(b *testing.B) {
	defer fixtures.Clean()

	for _, f := range fixtures.ByTag(".git") {
		b.Run(f.URL, func(b *testing.B) {
			fs := f.DotGit()
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

						iter, err := NewPackfileIter(fs, f, idxf, t, false, 0)
						if err != nil {
							b.Fatal(err)
						}

						err = iter.ForEach(func(o plumbing.EncodedObject) error {
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
	defer fixtures.Clean()

	for _, f := range fixtures.ByTag(".git") {
		b.Run(f.URL, func(b *testing.B) {
			fs := f.DotGit()
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

						iter, err := NewPackfileIter(fs, f, idxf, t, false, 0)
						if err != nil {
							b.Fatal(err)
						}

						err = iter.ForEach(func(o plumbing.EncodedObject) error {
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
	defer fixtures.Clean()

	for _, f := range fixtures.Basic() {
		b.Run(f.URL, func(b *testing.B) {
			fs := f.DotGit()
			o := NewObjectStorage(dotgit.New(fs), cache.NewObjectLRUDefault())
			for i := 0; i < b.N; i++ {
				expected := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")
				obj, err := o.EncodedObject(plumbing.AnyObject, expected)
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
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	objectCache := cache.NewObjectLRUDefault()
	objectStorage := NewObjectStorage(dotgit.New(fs), objectCache)
	hash := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")

	// Assert the cache is empty initially
	_, ok := objectCache.Get(hash)
	s.False(ok)

	// Load the object
	obj, err := objectStorage.EncodedObject(plumbing.AnyObject, hash)
	s.NoError(err)
	s.Equal(hash, obj.Hash())

	// The object should've been cached during the load
	cachedObj, ok := objectCache.Get(hash)
	s.True(ok)
	s.Equal(obj, cachedObj)

	// Assert that both objects can be read and that they both produce the same bytes

	objReader, err := obj.Reader()
	s.NoError(err)
	objBytes, err := io.ReadAll(objReader)
	s.NoError(err)
	s.NotEqual(0, len(objBytes))
	err = objReader.Close()
	s.NoError(err)

	cachedObjReader, err := cachedObj.Reader()
	s.NoError(err)
	cachedObjBytes, err := io.ReadAll(cachedObjReader)
	s.NotEqual(0, len(cachedObjBytes))
	s.NoError(err)
	err = cachedObjReader.Close()
	s.NoError(err)

	s.Equal(objBytes, cachedObjBytes)
}

func (s *FsSuite) TestGetFromUnpackedDoesNotCacheLargeObjects() {
	fs := fixtures.ByTag(".git").ByTag("unpacked").One().DotGit()
	objectCache := cache.NewObjectLRUDefault()
	objectStorage := NewObjectStorageWithOptions(dotgit.New(fs), objectCache, Options{LargeObjectThreshold: 1})
	hash := plumbing.NewHash("f3dfe29d268303fc6e1bbce268605fc99573406e")

	// Assert the cache is empty initially
	_, ok := objectCache.Get(hash)
	s.False(ok)

	// Load the object
	obj, err := objectStorage.EncodedObject(plumbing.AnyObject, hash)
	s.NoError(err)
	s.Equal(hash, obj.Hash())

	// The object should not have been cached during the load
	_, ok = objectCache.Get(hash)
	s.False(ok)
}
