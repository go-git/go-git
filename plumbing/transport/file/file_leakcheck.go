//go:build leakcheck

package file

import (
	"fmt"
	"runtime"
)

// setupLeakCheck sets up leak detection for a file connection
func setupLeakCheck(c *fileConn) {
	// Capture stack trace at connection creation time
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])

	runtime.SetFinalizer(c, func(conn *fileConn) {
		if !conn.closed {
			panic(fmt.Sprintf(`

=== LEAK DETECTED ===
File transport connection was garbage collected without Close() being called!
This will cause resource leaks (pipes, goroutines, storage handles).
Always call defer func() { _ = conn.Close() }() after creating a connection.

Connection was created at:
%s
=====================

`, stack))
		}
	})
}
