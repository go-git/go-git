//go:build !plan9 && unix && !windows
// +build !plan9,unix,!windows

package git

import "fmt"

// preReceiveHook returns the bytes of a pre-receive hook script
// that prints m before exiting successfully
func preReceiveHook(m string) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\nprintf '%s'\n", m))
}
