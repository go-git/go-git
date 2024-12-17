package storage

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type LimitSuite struct{}

var _ = Suite(&LimitSuite{})

func (s *LimitSuite) TestLimit(c *C) {
	var got []plumbing.EncodedObject

	storer := Limit(&mockStorer{
		SetEncodedObjectFunc: func(obj plumbing.EncodedObject) (plumbing.Hash, error) {
			got = append(got, obj)
			return plumbing.ZeroHash, nil
		},
	}, 100)

	_, err := storer.SetEncodedObject(&mockEncodedObject{size: 40})
	c.Assert(err, IsNil)
	
	c.Assert(*storer.N, Equals, int64(60))
}

func (s *LimitSuite) TestLimitExceeded(c *C) {
	var got []plumbing.EncodedObject

	storer := Limit(&mockStorer{
		SetEncodedObjectFunc: func(obj plumbing.EncodedObject) (plumbing.Hash, error) {
			got = append(got, obj)
			return plumbing.ZeroHash, nil
		},
	}, 100)

	_, err := storer.SetEncodedObject(&mockEncodedObject{size: 40})
	c.Assert(err, IsNil)

	_, err = storer.SetEncodedObject(&mockEncodedObject{size: 70})
	c.Assert(err, Equals, ErrLimitExceeded)

	c.Assert(*storer.N, Equals, int64(60))
}

type mockStorer struct {
	Storer

	SetEncodedObjectFunc func(plumbing.EncodedObject) (plumbing.Hash, error)
}

func (m *mockStorer) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	return m.SetEncodedObjectFunc(obj)
}

type mockEncodedObject struct {
	plumbing.EncodedObject

	size int64
}

func (m *mockEncodedObject) Size() int64 { return m.size }
