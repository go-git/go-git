// Package packhandle owns the shared descriptor for one pack file inside
// go-git.
//
// A [PackHandle] reads from one pack: it produces streaming and
// random-access cursors over the .pack file via
// [PackHandle.OpenPackReader] and [PackHandle.OpenRandomReader],
// and validates the trailing hash through [PackHandle.PackHash]. The descriptor
// opens lazily, is shared across concurrent readers, and follows the configured
// pool or grace-period policy when no cursor uses it.
//
// The package is internal: consumers must not surface any
// packhandle identifier on their own exported APIs; hold
// *PackHandle as a private named field (embedding is forbidden
// because it leaks the method set).
package packhandle
