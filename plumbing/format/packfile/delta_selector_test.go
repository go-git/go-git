package packfile

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

type DeltaSelectorSuite struct {
	suite.Suite
	ds     *deltaSelector
	store  *memory.Storage
	hashes map[string]plumbing.Hash
}

func TestDeltaSelectorSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(DeltaSelectorSuite))
}

func (s *DeltaSelectorSuite) SetupTest() {
	s.store = memory.NewStorage()
	s.createTestObjects()
	s.ds = newDeltaSelector(s.store)
}

func (s *DeltaSelectorSuite) TestSort() {
	o1 := newObjectToPack(newObject(plumbing.BlobObject, []byte("00000")))
	o4 := newObjectToPack(newObject(plumbing.BlobObject, []byte("0000")))
	o6 := newObjectToPack(newObject(plumbing.BlobObject, []byte("00")))
	o9 := newObjectToPack(newObject(plumbing.BlobObject, []byte("0")))
	o8 := newObjectToPack(newObject(plumbing.TreeObject, []byte("000")))
	o2 := newObjectToPack(newObject(plumbing.TreeObject, []byte("00")))
	o3 := newObjectToPack(newObject(plumbing.TreeObject, []byte("0")))
	o5 := newObjectToPack(newObject(plumbing.CommitObject, []byte("0000")))
	o7 := newObjectToPack(newObject(plumbing.CommitObject, []byte("00")))

	toSort := []*ObjectToPack{o1, o2, o3, o4, o5, o6, o7, o8, o9}
	s.ds.sort(toSort)
	expected := []*ObjectToPack{o1, o4, o6, o9, o8, o2, o3, o5, o7}
	s.Equal(expected, toSort)
}

type testObject struct {
	id     string
	object plumbing.EncodedObject
}

var testObjects = []*testObject{{
	id: "base",
	object: newObject(plumbing.BlobObject,
		genBytes([]piece{{
			times: 1000,
			val:   "a",
		}, {
			times: 1000,
			val:   "b",
		}})),
}, {
	id: "smallBase",
	object: newObject(plumbing.BlobObject,
		genBytes([]piece{{
			times: 1,
			val:   "a",
		}, {
			times: 1,
			val:   "b",
		}, {
			times: 6,
			val:   "c",
		}})),
}, {
	id: "smallTarget",
	object: newObject(plumbing.BlobObject,
		genBytes([]piece{{
			times: 1,
			val:   "a",
		}, {
			times: 1,
			val:   "c",
		}})),
}, {
	id: "target",
	object: newObject(plumbing.BlobObject,
		genBytes([]piece{{
			times: 1000,
			val:   "a",
		}, {
			times: 1000,
			val:   "b",
		}, {
			times: 1000,
			val:   "c",
		}})),
}, {
	id: "o1",
	object: newObject(plumbing.BlobObject,
		genBytes([]piece{{
			times: 1000,
			val:   "a",
		}, {
			times: 1000,
			val:   "b",
		}})),
}, {
	id: "o2",
	object: newObject(plumbing.BlobObject,
		genBytes([]piece{{
			times: 1000,
			val:   "a",
		}, {
			times: 500,
			val:   "b",
		}})),
}, {
	id: "o3",
	object: newObject(plumbing.BlobObject,
		genBytes([]piece{{
			times: 1000,
			val:   "a",
		}, {
			times: 499,
			val:   "b",
		}})),
}, {
	id: "bigBase",
	object: newObject(plumbing.BlobObject,
		genBytes([]piece{{
			times: 1000000,
			val:   "a",
		}})),
}, {
	id: "treeType",
	object: newObject(plumbing.TreeObject,
		[]byte("I am a tree!")),
}}

func (s *DeltaSelectorSuite) createTestObjects() {
	s.hashes = make(map[string]plumbing.Hash)
	for _, o := range testObjects {
		h, err := s.store.SetEncodedObject(o.object)
		if err != nil {
			panic(err)
		}
		s.hashes[o.id] = h
	}
}

func (s *DeltaSelectorSuite) TestObjectsToPack() {
	// Different type
	hashes := []plumbing.Hash{s.hashes["base"], s.hashes["treeType"]}
	deltaWindowSize := uint(10)
	otp, err := s.ds.ObjectsToPack(hashes, deltaWindowSize)
	s.NoError(err)
	s.Len(otp, 2)
	s.Equal(s.store.Objects[s.hashes["base"]], otp[0].Object)
	s.Equal(s.store.Objects[s.hashes["treeType"]], otp[1].Object)

	// Size radically different
	hashes = []plumbing.Hash{s.hashes["bigBase"], s.hashes["target"]}
	otp, err = s.ds.ObjectsToPack(hashes, deltaWindowSize)
	s.NoError(err)
	s.Len(otp, 2)
	s.Equal(s.store.Objects[s.hashes["bigBase"]], otp[0].Object)
	s.Equal(s.store.Objects[s.hashes["target"]], otp[1].Object)

	// Delta Size Limit with no best delta yet
	hashes = []plumbing.Hash{s.hashes["smallBase"], s.hashes["smallTarget"]}
	otp, err = s.ds.ObjectsToPack(hashes, deltaWindowSize)
	s.NoError(err)
	s.Len(otp, 2)
	s.Equal(s.store.Objects[s.hashes["smallBase"]], otp[0].Object)
	s.Equal(s.store.Objects[s.hashes["smallTarget"]], otp[1].Object)

	// It will create the delta
	hashes = []plumbing.Hash{s.hashes["base"], s.hashes["target"]}
	otp, err = s.ds.ObjectsToPack(hashes, deltaWindowSize)
	s.NoError(err)
	s.Len(otp, 2)
	s.Equal(s.store.Objects[s.hashes["target"]], otp[0].Object)
	s.False(otp[0].IsDelta())
	s.Equal(s.store.Objects[s.hashes["base"]], otp[1].Original)
	s.True(otp[1].IsDelta())
	s.Equal(1, otp[1].Depth)

	// If our base is another delta, the depth will increase by one
	hashes = []plumbing.Hash{
		s.hashes["o1"],
		s.hashes["o2"],
		s.hashes["o3"],
	}
	otp, err = s.ds.ObjectsToPack(hashes, deltaWindowSize)
	s.NoError(err)
	s.Len(otp, 3)
	s.Equal(s.store.Objects[s.hashes["o1"]], otp[0].Object)
	s.False(otp[0].IsDelta())
	s.Equal(s.store.Objects[s.hashes["o2"]], otp[1].Original)
	s.True(otp[1].IsDelta())
	s.Equal(1, otp[1].Depth)
	s.Equal(s.store.Objects[s.hashes["o3"]], otp[2].Original)
	s.True(otp[2].IsDelta())
	s.Equal(2, otp[2].Depth)

	// Check that objects outside of the sliding window don't produce
	// a delta.
	hashes = make([]plumbing.Hash, 0, deltaWindowSize+2)
	hashes = append(hashes, s.hashes["base"])
	for range deltaWindowSize {
		hashes = append(hashes, s.hashes["smallTarget"])
	}
	hashes = append(hashes, s.hashes["target"])

	// Don't sort so we can easily check the sliding window without
	// creating a bunch of new objects.
	otp, err = s.ds.objectsToPack(hashes, deltaWindowSize)
	s.NoError(err)
	err = s.ds.walk(otp, deltaWindowSize)
	s.NoError(err)
	s.Len(otp, int(deltaWindowSize)+2)
	targetIdx := len(otp) - 1
	s.False(otp[targetIdx].IsDelta())

	// Check that no deltas are created, and the objects are unsorted,
	// if compression is off.
	hashes = []plumbing.Hash{s.hashes["base"], s.hashes["target"]}
	otp, err = s.ds.ObjectsToPack(hashes, 0)
	s.NoError(err)
	s.Len(otp, 2)
	s.Equal(s.store.Objects[s.hashes["base"]], otp[0].Object)
	s.False(otp[0].IsDelta())
	s.Equal(s.store.Objects[s.hashes["target"]], otp[1].Original)
	s.False(otp[1].IsDelta())
	s.Equal(0, otp[1].Depth)
}

func (s *DeltaSelectorSuite) TestMaxDepth() {
	dsl := s.ds.deltaSizeLimit(0, 0, int(maxDepth), true)
	s.Equal(int64(0), dsl)
}
