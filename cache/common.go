package cache

import "gopkg.in/src-d/go-git.v4/plumbing"

const (
	Byte = 1 << (iota * 10)
	KiByte
	MiByte
	GiByte
)

type Object interface {
	Add(o plumbing.EncodedObject)
	Get(k plumbing.Hash) plumbing.EncodedObject
	Clear()
}
