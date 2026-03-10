package packfile

import (
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
)

type deltaObject struct {
	plumbing.EncodedObject
	base    plumbing.Hash
	baseOfs int64
	hash    plumbing.Hash
	size    int64
	index   idxfile.Index
}

// very ugly buuut works
func NewDeltaObject(
	obj plumbing.EncodedObject,
	hash plumbing.Hash,
	base plumbing.Hash,
	baseOfs int64,
	size int64,
	index idxfile.Index,
) plumbing.DeltaObject {
	return &deltaObject{
		EncodedObject: obj,
		hash:          hash,
		base:          base,
		baseOfs:       baseOfs,
		size:          size,
		index:         index,
	}
}

// BaseHash returns the hash of the base object. For OFS_DELTA objects where
// the base hash was not provided at construction time, this lazily resolves
// the hash from the index. It's unlikely for this to be really needed.
func (o *deltaObject) BaseHash() plumbing.Hash {
	if o.base.IsZero() && o.baseOfs > 0 && o.index != nil {
		if h, err := o.index.FindHash(o.baseOfs); err == nil {
			o.base = h
		}
	}
	return o.base
}

func (o *deltaObject) ActualSize() int64 {
	return o.size
}

func (o *deltaObject) ActualHash() plumbing.Hash {
	return o.hash
}
