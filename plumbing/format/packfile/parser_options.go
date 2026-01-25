package packfile

import (
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ParserOption configures a Parser.
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

// WithHighMemoryMode optimises the parser for speed rather than
// for memory consumption, making the Parser faster from an execution
// time perspective, but yielding much more allocations, which in the
// long run could make the application slower due to GC pressure.
//
// When the parser is being used without a storage, this is enabled
// automatically, as it can't operate without it. Some storage types
// may no support low memory mode (i.e. memory storage), for storage
// types that do support it, this becomes an opt-in feature.
//
// When enabled the inflated content of all delta objects (ofs and ref)
// will be loaded into cache, making it faster to navigate through them.
// If the reader provided to the parser does not implement io.Seeker,
// full objects may also be loaded into memory.
func WithHighMemoryMode() ParserOption {
	return func(p *Parser) {
		p.lowMemoryMode = false
	}
}
