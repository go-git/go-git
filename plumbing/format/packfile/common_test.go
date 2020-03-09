package packfile

import (
	"bytes"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type CommonSuite struct{}

var _ = Suite(&CommonSuite{})

func (s *CommonSuite) TestEmptyUpdateObjectStorage(c *C) {
	var buf bytes.Buffer
	sto := memory.NewStorage()

	err := UpdateObjectStorage(sto, &buf)
	c.Assert(err, Equals, ErrEmptyPackfile)
}

func newObject(t plumbing.ObjectType, cont []byte) plumbing.EncodedObject {
	o := plumbing.MemoryObject{}
	o.SetType(t)
	o.SetSize(int64(len(cont)))
	o.Write(cont)

	return &o
}

type piece struct {
	val   string
	times int
}

func genBytes(elements []piece) []byte {
	var result []byte
	for _, e := range elements {
		for i := 0; i < e.times; i++ {
			result = append(result, e.val...)
		}
	}

	return result
}
