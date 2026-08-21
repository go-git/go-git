// Package sharedfile provides a refcounted file handle that opens
// lazily on first Acquire, shares the underlying file descriptor
// across concurrent acquirers, and closes the descriptor after a
// configurable grace period once the refcount drops to zero.
//
// This avoids holding file descriptors open indefinitely — which
// causes problems on Windows where open files cannot be deleted —
// while still sharing a single FD across concurrent readers and
// avoiding repeated open/close syscalls for sequential operations.
package sharedfile
