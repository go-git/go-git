package seekable_test

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/formats/packfile"
	"gopkg.in/src-d/go-git.v3/storage/memory"
	"gopkg.in/src-d/go-git.v3/storage/seekable"
	"gopkg.in/src-d/go-git.v3/storage/seekable/internal/gitdir"
	"gopkg.in/src-d/go-git.v3/utils/fs"

	"github.com/alcortesm/tgz"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type FsSuite struct{}

var _ = Suite(&FsSuite{})

var fixtures map[string]string // id to git dir paths (see initFixtures below)

func fixture(id string, c *C) string {
	path, ok := fixtures[id]
	c.Assert(ok, Equals, true, Commentf("fixture %q not found", id))

	return path
}

var initFixtures = [...]struct {
	id  string
	tgz string
}{
	{
		id:  "binary-relations",
		tgz: "internal/gitdir/fixtures/alcortesm-binary-relations.tgz",
	}, {
		id:  "binary-relations-no-idx",
		tgz: "internal/gitdir/fixtures/alcortesm-binary-relations-no-idx.tgz",
	}, {
		id:  "ref-deltas-no-idx",
		tgz: "internal/gitdir/fixtures/ref-deltas-no-idx.tgz",
	},
}

func (s *FsSuite) SetUpSuite(c *C) {
	fixtures = make(map[string]string, len(initFixtures))
	for _, init := range initFixtures {
		path, err := tgz.Extract(init.tgz)
		c.Assert(err, IsNil, Commentf("error extracting %s\n", init.tgz))
		fixtures[init.id] = path
	}
}

func (s *FsSuite) TearDownSuite(c *C) {
	for _, v := range fixtures {
		err := os.RemoveAll(v)
		c.Assert(err, IsNil, Commentf("error removing fixture %q\n", v))
	}
}

func (s *FsSuite) TestNewErrorNotFound(c *C) {
	fs := fs.NewOS()
	_, err := seekable.New(fs, "not_found/.git")
	c.Assert(err, Equals, gitdir.ErrNotFound)
}

func (s *FsSuite) TestHashNotFound(c *C) {
	path := fixture("binary-relations", c)

	fs := fs.NewOS()
	gitPath := fs.Join(path, ".git/")

	sto, err := seekable.New(fs, gitPath)
	c.Assert(err, IsNil)

	_, err = sto.Get(core.ZeroHash)
	c.Assert(err, Equals, core.ErrObjectNotFound)
}

func (s *FsSuite) TestGetCompareWithMemoryStorage(c *C) {
	for i, fixId := range [...]string{
		"binary-relations",
		"binary-relations-no-idx",
		"ref-deltas-no-idx",
	} {
		path := fixture(fixId, c)
		com := Commentf("at subtest %d, (fixture id = %q, extracted to %q)",
			i, fixId, path)

		fs := fs.NewOS()
		gitPath := fs.Join(path, ".git/")

		memSto, err := memStorageFromGitDir(fs, gitPath)
		c.Assert(err, IsNil, com)

		seekableSto, err := seekable.New(fs, gitPath)
		c.Assert(err, IsNil, com)

		equal, reason, err := equalsStorages(memSto, seekableSto)
		c.Assert(err, IsNil, com)
		c.Assert(equal, Equals, true,
			Commentf("%s - %s\n", com.CheckCommentString(), reason))
	}
}

func memStorageFromGitDir(fs fs.FS, path string) (*memory.ObjectStorage, error) {
	dir, err := gitdir.New(fs, path)
	if err != nil {
		return nil, err
	}

	fs, packfilePath, err := dir.Packfile()
	if err != nil {
		return nil, err
	}

	f, err := fs.Open(packfilePath)
	if err != nil {
		return nil, err
	}

	sto := memory.NewObjectStorage()
	r := packfile.NewStream(f)
	d := packfile.NewDecoder(r)
	err = d.Decode(sto)
	if err != nil {
		return nil, err
	}

	err = f.Close()
	if err != nil {
		return nil, err
	}

	return sto, nil
}

func equalsStorages(a, b core.ObjectStorage) (bool, string, error) {
	for _, typ := range [...]core.ObjectType{
		core.CommitObject,
		core.TreeObject,
		core.BlobObject,
		core.TagObject,
	} {
		iter, err := a.Iter(typ)
		if err != nil {
			return false, "", fmt.Errorf("cannot get iterator: %s", err)
		}

		for {
			ao, err := iter.Next()
			if err != nil {
				iter.Close()
				break
			}

			bo, err := b.Get(ao.Hash())
			if err != nil {
				return false, "", fmt.Errorf("getting object with hash %s: %s",
					ao.Hash(), err)
			}

			equal, reason, err := equalsObjects(ao, bo)
			if !equal || err != nil {
				return equal, reason, fmt.Errorf("comparing objects: %s", err)
			}
		}
	}

	return true, "", nil
}

func equalsObjects(a, b core.Object) (bool, string, error) {
	ah := a.Hash()
	bh := b.Hash()
	if ah != bh {
		return false, fmt.Sprintf("object hashes differ: %s and %s\n",
			ah, bh), nil
	}

	atyp := a.Type()
	btyp := b.Type()
	if atyp != btyp {
		return false, fmt.Sprintf("object types differ: %d and %d\n",
			atyp, btyp), nil
	}

	asz := a.Size()
	bsz := b.Size()
	if asz != bsz {
		return false, fmt.Sprintf("object sizes differ: %d and %d\n",
			asz, bsz), nil
	}

	ac := a.Content()
	if ac != nil {
		bc := b.Content()
		if !reflect.DeepEqual(ac, bc) {
			return false, fmt.Sprintf("object contents differ"), nil
		}
	}

	return true, "", nil
}

func (s *FsSuite) TestIterCompareWithMemoryStorage(c *C) {
	for i, fixId := range [...]string{
		"binary-relations",
		"binary-relations-no-idx",
		"ref-deltas-no-idx",
	} {

		path := fixture(fixId, c)
		com := Commentf("at subtest %d, (fixture id = %q, extracted to %q)",
			i, fixId, path)

		fs := fs.NewOS()
		gitPath := fs.Join(path, ".git/")

		memSto, err := memStorageFromDirPath(fs, gitPath)
		c.Assert(err, IsNil, com)

		seekableSto, err := seekable.New(fs, gitPath)
		c.Assert(err, IsNil, com)

		for _, typ := range [...]core.ObjectType{
			core.CommitObject,
			core.TreeObject,
			core.BlobObject,
			core.TagObject,
		} {

			memObjs, err := iterToSortedSlice(memSto, typ)
			c.Assert(err, IsNil, com)

			seekableObjs, err := iterToSortedSlice(seekableSto, typ)
			c.Assert(err, IsNil, com)

			for i, o := range memObjs {
				c.Assert(seekableObjs[i].Hash(), Equals, o.Hash(), com)
			}
		}
	}
}

func memStorageFromDirPath(fs fs.FS, path string) (*memory.ObjectStorage, error) {
	dir, err := gitdir.New(fs, path)
	if err != nil {
		return nil, err
	}

	fs, packfilePath, err := dir.Packfile()
	if err != nil {
		return nil, err
	}

	sto := memory.NewObjectStorage()
	f, err := fs.Open(packfilePath)
	if err != nil {
		return nil, err
	}

	r := packfile.NewStream(f)
	d := packfile.NewDecoder(r)
	err = d.Decode(sto)
	if err != nil {
		return nil, err
	}

	if err = f.Close(); err != nil {
		return nil, err
	}

	return sto, nil
}

func iterToSortedSlice(storage core.ObjectStorage, typ core.ObjectType) ([]core.Object,
	error) {

	iter, err := storage.Iter(typ)
	if err != nil {
		return nil, err
	}

	r := make([]core.Object, 0)
	for {
		obj, err := iter.Next()
		if err != nil {
			iter.Close()
			break
		}
		r = append(r, obj)
	}

	sort.Sort(byHash(r))

	return r, nil
}

type byHash []core.Object

func (a byHash) Len() int      { return len(a) }
func (a byHash) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byHash) Less(i, j int) bool {
	return a[i].Hash().String() < a[j].Hash().String()
}

func (s *FsSuite) TestSet(c *C) {
	path := fixture("binary-relations", c)

	fs := fs.NewOS()
	gitPath := fs.Join(path, ".git/")

	sto, err := seekable.New(fs, gitPath)
	c.Assert(err, IsNil)

	_, err = sto.Set(&memory.Object{})
	c.Assert(err, ErrorMatches, "not implemented yet")
}
