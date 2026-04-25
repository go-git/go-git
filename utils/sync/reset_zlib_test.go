package sync

import (
	"sync"

	_ "unsafe"

	"github.com/go-git/go-git/v6/x/plugin"
)

//go:linkname resetPluginEntry github.com/go-git/go-git/v6/x/plugin.resetEntry
func resetPluginEntry(name plugin.Name)

// ResetZlibForTest resets the zlib plugin registration and local cached
// provider/pools so tests can control initialization deterministically.
func ResetZlibForTest() {
	resetPluginEntry("zlib")
	zlibProviderOnce = sync.Once{}
	zlibProvider = nil
	zlibReader = sync.Pool{New: newPooledZlibReader}
	zlibWriter = sync.Pool{New: newPooledZlibWriter}
}
