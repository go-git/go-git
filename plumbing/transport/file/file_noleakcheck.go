//go:build !leakcheck

package file

// setupLeakCheck is a no-op when leak checking is disabled
func setupLeakCheck(_ *fileConn) {}
