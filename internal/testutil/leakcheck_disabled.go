//go:build !leakcheck

package testutil

// TriggerLeakDetection is a no-op when leak detection is not enabled.
func TriggerLeakDetection() {}

// For internal use by RunWithLeakCheck
func triggerLeakDetection() {
	TriggerLeakDetection()
}
