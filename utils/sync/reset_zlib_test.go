package sync

import (
	"sync"

	_ "unsafe"

	"github.com/go-git/go-git/v6/x/plugin"
)

//go:linkname resetPluginEntry github.com/go-git/go-git/v6/x/plugin.resetEntry
func resetPluginEntry(name plugin.Name)

// ResetZlibForTest unfreezes the zlib plugin entry, reseeds the
// provider-resolution cache, and replaces the reader and writer pools
// so previously-pooled instances (built against the old provider) are
// dropped. Tests call this before installing a custom provider and
// during t.Cleanup so later tests see a fresh state.
//
// This function lives in a _test.go file and is only accessible to
// test binaries; production builds never see it.
func ResetZlibForTest() {
	resetPluginEntry("zlib")
	resolvedProvider = sync.OnceValue(func() plugin.ZlibProvider {
		p, err := plugin.Get(plugin.Zlib())
		if err != nil {
			panic("utils/sync: no zlib provider registered in x/plugin: " + err.Error())
		}
		return p
	})
	zlibReader = sync.Pool{New: newPooledZlibReader}
	zlibWriter = sync.Pool{New: newPooledZlibWriter}
}
