//go:build leakcheck

package git

import (
	"fmt"
	"runtime"
)

// setupLeakCheck sets up leak detection for a repository
func setupLeakCheck(r *Repository) {
	// Capture stack trace at repository creation time
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])

	runtime.SetFinalizer(r, func(repo *Repository) {
		if !repo.closed {
			panic(fmt.Sprintf(`

=== LEAK DETECTED ===
Repository was garbage collected without Close() being called!
This will cause file handle leaks on Windows.
Always call defer func() { _ = repo.Close() }() after creating a repository.

Repository was created at:
%s
=====================

`, stack))
		}
	})
}
