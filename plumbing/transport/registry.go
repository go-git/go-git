package transport

import (
	"fmt"
	"sync"
)

// registry are the protocols supported by default.
var (
	registry = map[string]Transport{}
	mtx      sync.Mutex
)

// Register adds or modifies an existing protocol.
// Equivalent to client.InstallProtocol in go-git before V6.
func Register(protocol string, c Transport) {
	mtx.Lock()
	registry[protocol] = c
	mtx.Unlock()
}

// Unregister removes a protocol from the list of supported protocols.
func Unregister(scheme string) {
	mtx.Lock()
	delete(registry, scheme)
	mtx.Unlock()
}

// Get returns the appropriate client for the given protocol.
func Get(p string) (Transport, error) {
	f, ok := registry[p]
	if !ok {
		return nil, fmt.Errorf("unsupported scheme %q", p)
	}

	if f == nil {
		return nil, fmt.Errorf("malformed client for scheme %q, client is defined as nil", p)
	}
	return f, nil
}
