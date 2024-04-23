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
func Register(protocol string, c Transport) {
	mtx.Lock()
	defer mtx.Unlock()
	registry[protocol] = c
}

// Unregister removes a protocol from the list of supported protocols.
func Unregister(scheme string) {
	mtx.Lock()
	defer mtx.Unlock()
	delete(registry, scheme)
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
