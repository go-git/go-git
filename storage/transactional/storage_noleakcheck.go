//go:build !leakcheck

package transactional

// setupLeakCheck is a no-op when leak checking is disabled
func setupLeakCheck(*basic) {}
