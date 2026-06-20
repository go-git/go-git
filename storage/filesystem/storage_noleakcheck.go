//go:build !leakcheck

package filesystem

// setupLeakCheck is a no-op when leak checking is disabled
func setupLeakCheck(_ *Storage) {
	// No-op
}
