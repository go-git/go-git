//go:build leakcheck

package transactional

import (
	"fmt"
	"runtime"
)

// setupLeakCheck sets up leak detection for a transactional storage
func setupLeakCheck(s *basic) {
	// Capture stack trace at storage creation time
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])

	runtime.SetFinalizer(s, func(storage *basic) {
		if !storage.closed {
			panic(fmt.Sprintf(`

=== TRANSACTIONAL STORAGE LEAK DETECTED ===
Transactional storage was garbage collected without Close() being called!
This will cause file handle leaks on Windows if wrapping filesystem storage.
Always call defer func() { _ = storage.Close() }() after creating a transactional storage.

Storage was created at:
%s
=====================

`, stack))
		}
	})
}
