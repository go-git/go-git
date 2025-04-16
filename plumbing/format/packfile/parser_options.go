package packfile

import (
	"github.com/go-git/go-git/v6/plumbing/storer"
)

type ParserOption func(*Parser)

// WithStorage sets the storage to be used while parsing a pack file.
func WithStorage(storage storer.EncodedObjectStorer) ParserOption {
	return func(p *Parser) {
		p.storage = storage
	}
}

// WithScannerObservers sets the observers to be notified during the
// scanning or parsing of a pack file. The scanner is responsible for
// notifying observers around general pack file information, such as
// header and footer. The scanner also notifies object headers for
// non-delta objects.
//
// Delta objects are notified as part of the parser logic.
func WithScannerObservers(ob ...Observer) ParserOption {
	return func(p *Parser) {
		p.observers = ob
	}
}

// WithHighMemoryUsage makes the parser optimise for speed rather than
// for memory consumption. This is disabled by default.
//
// When enabled the inflated content of all delta objects (ofs and ref)
// will be loaded into cache, making it faster to navigate through them.
func WithHighMemoryUsage() ParserOption {
	return func(p *Parser) {
		p.lowMemory = false
	}
}
