package plugin

import (
	xzlib "github.com/go-git/go-git/v6/x/plugin/zlib"
)

func init() {
	// Registers the stdlib-backed zlib provider by default, aligning
	// go-git's behaviour with its historical use of compress/zlib.
	_ = Register(Zlib(), func() ZlibProvider {
		return xzlib.NewStdlib()
	})
}

const zlibPlugin Name = "zlib"

var zlibKey = newKeyWithValidator(zlibPlugin, xzlib.ValidateProvider)

// ZlibReader is the method set required of a zlib decompression reader.
// See [xzlib.Reader] for the full contract.
type ZlibReader = xzlib.Reader

// ZlibWriter is the method set required of a zlib compression writer.
// See [xzlib.Writer] for the full contract.
type ZlibWriter = xzlib.Writer

// ZlibProvider constructs zlib readers and writers. See
// [xzlib.Provider] for the full contract.
type ZlibProvider = xzlib.Provider

// Zlib returns the key used to register a zlib provider plugin. When
// set, go-git uses this plugin to construct zlib readers and writers
// instead of the built-in stdlib provider.
func Zlib() key[ZlibProvider] {
	return zlibKey
}
