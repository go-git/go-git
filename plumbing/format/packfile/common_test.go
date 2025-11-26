package packfile

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestEmptyUpdateObjectStorage(t *testing.T) {
	var buf bytes.Buffer
	sto := memory.NewStorage()

	err := UpdateObjectStorage(sto, &buf)
	assert.ErrorIs(t, err, ErrEmptyPackfile)
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
