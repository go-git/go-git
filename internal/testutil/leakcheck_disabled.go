//go:build !leakcheck

package testutil

// triggerLeakDetection is a no-op when leak detection is not enabled.
func triggerLeakDetection() {}
