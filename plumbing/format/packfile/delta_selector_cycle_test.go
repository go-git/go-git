package packfile

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

// cyclicDelta is a delta object whose base hash is set by the test, allowing
// delta chains that loop back on themselves to be built.
type cyclicDelta struct {
	plumbing.EncodedObject
	self plumbing.Hash
	base plumbing.Hash
}

func (d *cyclicDelta) Type() plumbing.ObjectType { return plumbing.OFSDeltaObject }
func (d *cyclicDelta) BaseHash() plumbing.Hash   { return d.base }
func (d *cyclicDelta) ActualHash() plumbing.Hash { return d.self }
func (d *cyclicDelta) ActualSize() int64         { return d.Size() }

type DeltaSelectorCycleSuite struct {
	suite.Suite
	store *memory.Storage
	ds    *deltaSelector
}

func TestDeltaSelectorCycleSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(DeltaSelectorCycleSuite))
}

func (s *DeltaSelectorCycleSuite) SetupTest() {
	s.store = memory.NewStorage()
	s.ds = newDeltaSelector(s.store)
}

// addObject stores a non-delta object so that undeltify can restore it.
func (s *DeltaSelectorCycleSuite) addObject(content string) plumbing.EncodedObject {
	o := newObject(plumbing.BlobObject, []byte(content))
	_, err := s.store.SetEncodedObject(o)
	s.Require().NoError(err)
	return o
}

// deltaOn returns an ObjectToPack for a delta of o based on base.
func (s *DeltaSelectorCycleSuite) deltaOn(o plumbing.EncodedObject, base plumbing.Hash) *ObjectToPack {
	return &ObjectToPack{
		Object: &cyclicDelta{EncodedObject: o, self: o.Hash(), base: base},
	}
}

// assertResolvable walks each object's Base pointers and fails if a cycle is
// still reachable or the chain does not terminate at a non-delta object.
func (s *DeltaSelectorCycleSuite) assertResolvable(objs []*ObjectToPack) {
	for _, otp := range objs {
		seen := make(map[plumbing.Hash]bool)
		for cur := otp; cur != nil; cur = cur.Base {
			h := cur.Hash()
			s.Require().False(seen[h], "delta chain still contains a cycle at %s", h)
			seen[h] = true
		}
		// A delta must have a base to be writable.
		if otp.Object.Type().IsDelta() {
			s.Require().NotNil(otp.Base, "delta %s left without a base", otp.Hash())
		}
	}
}

// A delta whose base is itself must not recurse forever.
func (s *DeltaSelectorCycleSuite) TestSelfReferencingDelta() {
	a := s.addObject("aaaaaaaa")
	otpA := s.deltaOn(a, a.Hash())

	objs := []*ObjectToPack{otpA}
	s.Require().NoError(s.ds.fixAndBreakChains(objs))

	// The only way to break a self-cycle is to store the object whole.
	s.False(otpA.Object.Type().IsDelta(), "self-referencing delta should be undeltified")
	s.Nil(otpA.Base)
	s.assertResolvable(objs)
}

// Two deltas based on each other must not recurse forever. This is the case
// reported in issue #2249.
func (s *DeltaSelectorCycleSuite) TestTwoObjectCycle() {
	a := s.addObject("aaaaaaaa")
	b := s.addObject("bbbbbbbb")

	otpA := s.deltaOn(a, b.Hash())
	otpB := s.deltaOn(b, a.Hash())

	objs := []*ObjectToPack{otpA, otpB}
	s.Require().NoError(s.ds.fixAndBreakChains(objs))

	s.assertResolvable(objs)

	// Exactly one of the two must have been undeltified to break the cycle;
	// the other stays a delta so pack size is not sacrificed needlessly.
	deltas := 0
	for _, otp := range objs {
		if otp.Object.Type().IsDelta() {
			deltas++
		}
	}
	s.Equal(1, deltas, "exactly one object should be undeltified")
}

// A longer cycle (A -> B -> C -> A) must also terminate.
func (s *DeltaSelectorCycleSuite) TestThreeObjectCycle() {
	a := s.addObject("aaaaaaaa")
	b := s.addObject("bbbbbbbb")
	c := s.addObject("cccccccc")

	otpA := s.deltaOn(a, b.Hash())
	otpB := s.deltaOn(b, c.Hash())
	otpC := s.deltaOn(c, a.Hash())

	objs := []*ObjectToPack{otpA, otpB, otpC}
	s.Require().NoError(s.ds.fixAndBreakChains(objs))

	s.assertResolvable(objs)

	deltas := 0
	for _, otp := range objs {
		if otp.Object.Type().IsDelta() {
			deltas++
		}
	}
	s.Equal(2, deltas, "only one object should be undeltified to break the cycle")
}

// A valid, acyclic chain must keep every delta: the cycle check must not
// produce false positives on diamonds or repeated bases.
func (s *DeltaSelectorCycleSuite) TestAcyclicChainIsPreserved() {
	base := s.addObject("basebasebase")
	a := s.addObject("aaaaaaaa")
	b := s.addObject("bbbbbbbb")

	// base is a plain object; both a and b are deltas against it.
	otpBase := newObjectToPack(base)
	otpA := s.deltaOn(a, base.Hash())
	otpB := s.deltaOn(b, base.Hash())

	objs := []*ObjectToPack{otpBase, otpA, otpB}
	s.Require().NoError(s.ds.fixAndBreakChains(objs))

	s.True(otpA.Object.Type().IsDelta(), "a should remain a delta")
	s.True(otpB.Object.Type().IsDelta(), "b should remain a delta")
	s.Equal(otpBase, otpA.Base)
	s.Equal(otpBase, otpB.Base)
	s.assertResolvable(objs)
}

// A deep chain shares bases across separate resolution paths; the visiting set
// must be unwound correctly so no delta is dropped.
func (s *DeltaSelectorCycleSuite) TestDeepChainIsPreserved() {
	const n = 50

	objs := make([]*ObjectToPack, 0, n)
	prev := s.addObject("root")
	objs = append(objs, newObjectToPack(prev))

	for i := 1; i < n; i++ {
		o := s.addObject(string(rune('a'+i%26)) + "-payload-" + string(rune('0'+i%10)))
		objs = append(objs, s.deltaOn(o, prev.Hash()))
		prev = o
	}

	s.Require().NoError(s.ds.fixAndBreakChains(objs))
	s.assertResolvable(objs)

	for _, otp := range objs[1:] {
		s.True(otp.Object.Type().IsDelta(), "delta %s was dropped", otp.Hash())
	}
}
