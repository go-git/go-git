//go:build !leakcheck

package git

// setupLeakCheck is a no-op when leak checking is disabled
func setupLeakCheck(_ *Repository) {
	// No-op
}
