// Package access exposes unexported operations of the
// plumbing/format/packfile package to other go-git packages, without
// adding them to the public packfile API.
//
// The hooks are set by the packfile package during initialisation, so
// any package that imports packfile can rely on them being non-nil.
package access

import "github.com/go-git/go-git/v6/plumbing"

// DeltaObjectAt reads the object stored at offset in p, which must be a
// *packfile.Packfile.
//
// When the entry is a delta, obj holds the undecoded delta data with
// the delta type, base is the hash of the delta base and size is the
// size declared in the entry header. Otherwise obj is the fully
// resolved object and base is the zero hash.
var DeltaObjectAt func(p any, offset int64) (obj plumbing.EncodedObject, base plumbing.Hash, size int64, err error)
