//go:build leakcheck

package filesystem

import (
	"fmt"
	"runtime"
)

// setupLeakCheck sets up leak detection for a storage
func setupLeakCheck(s *Storage) {
	// Capture stack trace at storage creation time
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])

	runtime.SetFinalizer(s, func(storage *Storage) {
		if !storage.closed {
			panic(fmt.Sprintf(`

=== STORAGE LEAK DETECTED ===
Storage was garbage collected without Close() being called!
This will cause file handle leaks on Windows.
Always call defer func() { _ = storage.Close() }() after creating a storage.

Storage was created at:
%s
=====================

`, stack))
		}
	})
}
