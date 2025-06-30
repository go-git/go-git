package tests

import (
	"fmt"
	"io"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/go-git/go-git/v6/storage/transactional"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Storer interface {
	storer.EncodedObjectStorer
	storer.ReferenceStorer
	storer.ShallowStorer
	storer.IndexStorer
	config.ConfigStorer
	storage.ModuleStorer
}

type TestObject struct {
	Object plumbing.EncodedObject
	Hash   string
	Type   plumbing.ObjectType
}

func testObjects() map[plumbing.ObjectType]TestObject {
	commit := &plumbing.MemoryObject{}
	commit.SetType(plumbing.CommitObject)
	tree := &plumbing.MemoryObject{}
	tree.SetType(plumbing.TreeObject)
	blob := &plumbing.MemoryObject{}
	blob.SetType(plumbing.BlobObject)
	tag := &plumbing.MemoryObject{}
	tag.SetType(plumbing.TagObject)

	return map[plumbing.ObjectType]TestObject{
		plumbing.CommitObject: {commit, "dcf5b16e76cce7425d0beaef62d79a7d10fce1f5", plumbing.CommitObject},
		plumbing.TreeObject:   {tree, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", plumbing.TreeObject},
		plumbing.BlobObject:   {blob, "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", plumbing.BlobObject},
		plumbing.TagObject:    {tag, "d994c6bb648123a17e8f70a966857c546b2a6f94", plumbing.TagObject},
	}
}

func validTypes() []plumbing.ObjectType {
	return []plumbing.ObjectType{
		plumbing.CommitObject,
		plumbing.BlobObject,
		plumbing.TagObject,
		plumbing.TreeObject,
	}
}

var storageFactories = []func(t *testing.T) (Storer, string){
	func(_ *testing.T) (Storer, string) { return memory.NewStorage(), "memory" },
	func(t *testing.T) (Storer, string) {
		return filesystem.NewStorage(osfs.New(t.TempDir()), nil), "filesystem"
	},
	func(t *testing.T) (Storer, string) {
		temporal := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())
		base := memory.NewStorage()

		return transactional.NewStorage(base, temporal), "transactional"
	},
}

func forEachStorage(t *testing.T, tc func(sto Storer, t *testing.T)) {
	for _, factory := range storageFactories {
		sto, name := factory(t)

		t.Run(name, func(t *testing.T) {
			tc(sto, t)
		})
	}
}

func TestPackfileWriter(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		pwr, ok := sto.(storer.PackfileWriter)
		if !ok {
			t.Skip("not a PackfileWriter")
		}

		pw, err := pwr.PackfileWriter()
		assert.NoError(t, err)

		f := fixtures.Basic().One()
		_, err = io.Copy(pw, f.Packfile())
		assert.NoError(t, err)

		err = pw.Close()
		assert.NoError(t, err)

		iter, err := sto.IterEncodedObjects(plumbing.AnyObject)
		assert.NoError(t, err)
		objects := 0

		err = iter.ForEach(func(plumbing.EncodedObject) error {
			objects++
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, 31, objects)
	})
}

func TestDeltaObjectStorer(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		dos, ok := sto.(storer.DeltaObjectStorer)
		if !ok {
			t.Skip("not an DeltaObjectStorer")
		}

		pwr, ok := sto.(storer.PackfileWriter)
		if !ok {
			t.Skip("not a storer.PackWriter")
		}

		pw, err := pwr.PackfileWriter()
		require.NoError(t, err)

		f := fixtures.Basic().One()
		_, err = io.Copy(pw, f.Packfile())
		require.NoError(t, err)

		err = pw.Close()
		require.NoError(t, err)

		h := plumbing.NewHash("32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
		obj, err := dos.DeltaObject(plumbing.AnyObject, h)
		require.NoError(t, err)
		assert.Equal(t, plumbing.BlobObject, obj.Type())

		h = plumbing.NewHash("aa9b383c260e1d05fbbf6b30a02914555e20c725")
		obj, err = dos.DeltaObject(plumbing.AnyObject, h)
		require.NoError(t, err)
		assert.Equal(t, plumbing.OFSDeltaObject.String(), obj.Type().String())

		_, ok = obj.(plumbing.DeltaObject)
		assert.True(t, ok)
	})
}

func TestSetEncodedObjectAndEncodedObject(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		for _, to := range testObjects() {
			comment := fmt.Sprintf("failed for type %s", to.Type.String())

			h, err := sto.SetEncodedObject(to.Object)
			require.NoError(t, err)
			require.Equal(t, to.Hash, h.String(), comment)

			o, err := sto.EncodedObject(to.Type, h)
			require.NoError(t, err)
			assert.Equal(t, to.Object, o)

			o, err = sto.EncodedObject(plumbing.AnyObject, h)
			require.NoError(t, err)
			assert.Equal(t, to.Object, o)

			for _, typ := range validTypes() {
				if typ == to.Type {
					continue
				}

				o, err = sto.EncodedObject(typ, h)
				assert.Nil(t, o)
				assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
			}
		}
	})
}

func TestSetEncodedObjectInvalid(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		o := sto.NewEncodedObject()
		o.SetType(plumbing.REFDeltaObject)

		_, err := sto.SetEncodedObject(o)
		assert.Error(t, err)
	})
}

func TestIterEncodedObjects(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		objs := testObjects()
		for _, o := range objs {
			h, err := sto.SetEncodedObject(o.Object)
			require.NoError(t, err)
			assert.Equal(t, o.Object.Hash(), h)
		}

		for _, typ := range validTypes() {
			comment := fmt.Sprintf("failed for type %s)", typ.String())
			i, err := sto.IterEncodedObjects(typ)
			require.NoError(t, err, comment)

			o, err := i.Next()
			require.NoError(t, err)
			assert.Equal(t, objs[typ].Object, o)

			o, err = i.Next()
			assert.Nil(t, o)
			assert.ErrorIs(t, err, io.EOF, comment)
		}

		i, err := sto.IterEncodedObjects(plumbing.AnyObject)
		require.NoError(t, err)

		foundObjects := []plumbing.EncodedObject{}
		i.ForEach(func(o plumbing.EncodedObject) error {
			foundObjects = append(foundObjects, o)
			return nil
		})

		assert.Len(t, foundObjects, len(testObjects()))
		for _, to := range testObjects() {
			found := false
			for _, o := range foundObjects {
				if to.Object.Hash() == o.Hash() {
					found = true
					break
				}
			}
			assert.True(t, found, "Object of type %s not found", to.Type.String())
		}
	})
}

func TestObjectStorerTxSetEncodedObjectAndCommit(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		storer, ok := sto.(storer.Transactioner)
		if !ok {
			t.Skip("not a plumbing.ObjectStorerTx")
		}

		tx := storer.Begin()
		for _, o := range testObjects() {
			h, err := tx.SetEncodedObject(o.Object)
			require.NoError(t, err)
			assert.Equal(t, o.Hash, h.String())
		}

		iter, err := sto.IterEncodedObjects(plumbing.AnyObject)
		require.NoError(t, err)
		_, err = iter.Next()
		assert.ErrorIs(t, err, io.EOF)

		err = tx.Commit()
		require.NoError(t, err)

		iter, err = sto.IterEncodedObjects(plumbing.AnyObject)
		require.NoError(t, err)

		var count int
		iter.ForEach(func(o plumbing.EncodedObject) error {
			count++
			return nil
		})

		assert.Equal(t, 4, count)
	})
}

func TestObjectStorerTxSetObjectAndGetObject(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		storer, ok := sto.(storer.Transactioner)
		if !ok {
			t.Skip("not a plumbing.ObjectStorerTx")
		}

		tx := storer.Begin()
		for _, expected := range testObjects() {
			h, err := tx.SetEncodedObject(expected.Object)
			require.NoError(t, err)
			assert.Equal(t, expected.Hash, h.String())

			o, err := tx.EncodedObject(expected.Type, plumbing.NewHash(expected.Hash))
			require.NoError(t, err)
			assert.Equal(t, expected.Hash, o.Hash().String())
		}
	})
}

func TestObjectStorerTxGetObjectNotFound(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		storer, ok := sto.(storer.Transactioner)
		if !ok {
			t.Skip("not a plumbing.ObjectStorerTx")
		}

		tx := storer.Begin()
		o, err := tx.EncodedObject(plumbing.AnyObject, plumbing.ZeroHash)
		assert.Nil(t, o)
		assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)
	})
}

func TestObjectStorerTxSetObjectAndRollback(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		storer, ok := sto.(storer.Transactioner)
		if !ok {
			t.Skip("not a plumbing.ObjectStorerTx")
		}

		tx := storer.Begin()
		for _, o := range testObjects() {
			h, err := tx.SetEncodedObject(o.Object)
			require.NoError(t, err)
			assert.Equal(t, o.Hash, h.String())
		}

		err := tx.Rollback()
		require.NoError(t, err)

		iter, err := sto.IterEncodedObjects(plumbing.AnyObject)
		require.NoError(t, err)
		_, err = iter.Next()
		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestSetReferenceAndGetReference(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		err := sto.SetReference(
			plumbing.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		)
		require.NoError(t, err)

		err = sto.SetReference(
			plumbing.NewReferenceFromStrings("bar", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
		)
		require.NoError(t, err)

		e, err := sto.Reference(plumbing.ReferenceName("foo"))
		require.NoError(t, err)
		assert.Equal(t, e.Hash().String(), "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	})
}

func TestCheckAndSetReference(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		err := sto.SetReference(
			plumbing.NewReferenceFromStrings("foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
		)
		require.NoError(t, err)

		err = sto.CheckAndSetReference(
			plumbing.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
			plumbing.NewReferenceFromStrings("foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
		)
		require.NoError(t, err)

		e, err := sto.Reference(plumbing.ReferenceName("foo"))
		require.NoError(t, err)
		assert.Equal(t, e.Hash().String(), "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	})
}

func TestCheckAndSetReferenceNil(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		err := sto.SetReference(
			plumbing.NewReferenceFromStrings("foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
		)
		require.NoError(t, err)

		err = sto.CheckAndSetReference(
			plumbing.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
			nil,
		)
		require.NoError(t, err)

		e, err := sto.Reference(plumbing.ReferenceName("foo"))
		require.NoError(t, err)
		assert.Equal(t, e.Hash().String(), "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")
	})
}

func TestCheckAndSetReferenceError(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		err := sto.SetReference(
			plumbing.NewReferenceFromStrings("foo", "c3f4688a08fd86f1bf8e055724c84b7a40a09733"),
		)
		require.NoError(t, err)

		err = sto.CheckAndSetReference(
			plumbing.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
			plumbing.NewReferenceFromStrings("foo", "482e0eada5de4039e6f216b45b3c9b683b83bfa"),
		)
		assert.ErrorIs(t, err, storage.ErrReferenceHasChanged)

		e, err := sto.Reference(plumbing.ReferenceName("foo"))
		require.NoError(t, err)
		assert.Equal(t, e.Hash().String(), "c3f4688a08fd86f1bf8e055724c84b7a40a09733")
	})
}

func TestRemoveReference(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		err := sto.SetReference(
			plumbing.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		)
		require.NoError(t, err)

		err = sto.RemoveReference(plumbing.ReferenceName("foo"))
		require.NoError(t, err)

		_, err = sto.Reference(plumbing.ReferenceName("foo"))
		assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
	})
}

func TestRemoveReferenceNonExistent(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		err := sto.SetReference(
			plumbing.NewReferenceFromStrings("foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		)
		require.NoError(t, err)

		err = sto.RemoveReference(plumbing.ReferenceName("nonexistent"))
		require.NoError(t, err)

		e, err := sto.Reference(plumbing.ReferenceName("foo"))
		require.NoError(t, err)
		assert.Equal(t, "bc9968d75e48de59f0870ffb71f5e160bbbdcf52", e.Hash().String())
	})
}

func TestGetReferenceNotFound(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		r, err := sto.Reference(plumbing.ReferenceName("bar"))
		assert.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
		assert.Nil(t, r)
	})
}

func TestIterReferences(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		err := sto.SetReference(
			plumbing.NewReferenceFromStrings("refs/foo", "bc9968d75e48de59f0870ffb71f5e160bbbdcf52"),
		)
		require.NoError(t, err)

		i, err := sto.IterReferences()
		require.NoError(t, err)

		e, err := i.Next()
		require.NoError(t, err)
		assert.Equal(t, e.Hash().String(), "bc9968d75e48de59f0870ffb71f5e160bbbdcf52")

		e, err = i.Next()
		assert.Nil(t, e)
		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestSetShallowAndShallow(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		expected := []plumbing.Hash{
			plumbing.NewHash("b66c08ba28aa1f81eb06a1127aa3936ff77e5e2c"),
			plumbing.NewHash("c3f4688a08fd86f1bf8e055724c84b7a40a09733"),
			plumbing.NewHash("c78874f116be67ecf54df225a613162b84cc6ebf"),
		}

		err := sto.SetShallow(expected)
		require.NoError(t, err)

		result, err := sto.Shallow()
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})
}

func TestSetConfigAndConfig(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		expected := config.NewConfig()
		expected.Core.IsBare = true
		expected.Remotes["foo"] = &config.RemoteConfig{
			Name: "foo",
			URLs: []string{"http://foo/bar.git"},
		}

		err := sto.SetConfig(expected)
		require.NoError(t, err)

		cfg, err := sto.Config()
		require.NoError(t, err)

		assert.Equal(t, expected.Core.IsBare, cfg.Core.IsBare)
		assert.Equal(t, expected.Remotes, cfg.Remotes)
	})
}

func TestIndex(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		expected := &index.Index{}
		expected.Version = 2

		idx, err := sto.Index()
		assert.NoError(t, err)
		assert.Equal(t, expected, idx)
	})
}

func TestSetIndexAndIndex(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		expected := &index.Index{}
		expected.Version = 2

		err := sto.SetIndex(expected)
		require.NoError(t, err)

		idx, err := sto.Index()
		require.NoError(t, err)
		assert.Equal(t, expected, idx)
	})
}

func TestSetConfigInvalid(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		cfg := config.NewConfig()
		cfg.Remotes["foo"] = &config.RemoteConfig{}

		err := sto.SetConfig(cfg)
		assert.Error(t, err)
	})
}

func TestModule(t *testing.T) {
	t.Parallel()

	forEachStorage(t, func(sto Storer, t *testing.T) {
		storer, err := sto.Module("foo")
		require.NoError(t, err)
		assert.NotNil(t, storer)

		storer, err = sto.Module("foo")
		require.NoError(t, err)
		assert.NotNil(t, storer)
	})
}
