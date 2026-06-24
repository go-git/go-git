// Package packhandle owns the file descriptors for one pack
// triple (.pack + .idx + .rev) inside go-git.
//
// A [PackHandle] reads from one pack: it produces streaming and
// random-access cursors over the .pack file via
// [PackHandle.OpenPackReader] and [PackHandle.OpenRandomReader],
// a parsed [PackMeta] via [PackHandle.Meta], and an
// [idxfile.Index] over the .idx/.rev pair via [PackHandle.Index].
// The .pack file descriptor is opened lazily on first cursor
// request, shared across concurrent readers, and closed after a
// one-second idle grace period once no cursors remain. .idx and
// .rev descriptors are owned by the returned [idxfile.Index].
//
// The package is internal: consumers must not surface any
// packhandle identifier on their own exported APIs; hold
// *PackHandle as a private named field (embedding is forbidden
// because it leaks the method set).
package packhandle
