package plumbing

import (
	"crypto"
	"strconv"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/hash"
)

// ComputeHash compute the hash for a given ObjectType and content.
func ComputeHash256(t ObjectType, content []byte) ObjectID {
	h := NewHasher256(t, int64(len(content)))
	h.Write(content)
	return h.Sum()
}

type Hasher256 struct {
	hash.Hash
}

func NewHasher256(t ObjectType, size int64) Hasher256 {
	h := Hasher256{hash.New(crypto.SHA256)}
	h.Reset(t, size)
	return h
}

func (h Hasher256) Reset(t ObjectType, size int64) {
	h.Hash.Reset()
	h.Write(t.Bytes())
	h.Write([]byte(" "))
	h.Write([]byte(strconv.FormatInt(size, 10)))
	h.Write([]byte{0})
}

func (h Hasher256) Sum() (id ObjectID) {
	id.format = config.SHA256
	copy(id.hash[:], h.Hash.Sum(nil))
	return
}
