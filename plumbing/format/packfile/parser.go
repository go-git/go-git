package packfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	stdsync "sync"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
	"github.com/go-git/go-git/v6/utils/sync"
)

var (
	// ErrReferenceDeltaNotFound is returned when the reference delta is not
	// found.
	ErrReferenceDeltaNotFound = errors.New("reference delta not found")

	// ErrNotSeekableSource is returned when the source for the parser is not
	// seekable and a storage was not provided, so it can't be parsed.
	ErrNotSeekableSource = errors.New("parser source is not seekable and storage was not provided")

	// ErrDeltaNotCached is returned when the delta could not be found in cache.
	ErrDeltaNotCached = errors.New("delta could not be found in cache")
)

// Parser decodes a packfile and calls any observer associated to it. Is used
// to generate indexes.
type Parser struct {
	storage   storer.EncodedObjectStorer
	cache     *parserCache
	lowMemory bool

	scanner   *Scanner
	observers []Observer
	hasher    plumbing.Hasher

	checksum plumbing.Hash
	m        stdsync.Mutex
}

// NewParser creates a new Parser.
// When a storage is set, the objects are written to storage as they
// are parsed.
func NewParser(data io.Reader, opts ...ParserOption) *Parser {
	p := &Parser{
		hasher:    plumbing.NewHasher(format.SHA1, plumbing.AnyObject, 0),
		lowMemory: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}

	p.scanner = NewScanner(data)

	if p.storage != nil {
		p.scanner.storage = p.storage
	}
	p.cache = newParserCache()

	return p
}

func (p *Parser) storeOrCache(oh *ObjectHeader) error {
	// Only need to store deltas, as the scanner already stored non-delta
	// objects.
	if p.storage != nil && oh.diskType.IsDelta() {
		w, err := p.storage.RawObjectWriter(oh.Type, oh.Size)
		if err != nil {
			return err
		}

		defer w.Close()

		_, err = ioutil.Copy(w, oh.content)
		if err != nil {
			return err
		}
	}

	if p.cache != nil {
		o := oh
		for p.lowMemory && o.content != nil {
			sync.PutBytesBuffer(o.content)
			o.content = nil

			if o.parent == nil || o.parent.content == nil {
				break
			}
			o = o.parent
		}
		p.cache.Add(oh)
	}

	if err := p.onInflatedObjectHeader(oh.Type, oh.Size, oh.Offset); err != nil {
		return err
	}

	if err := p.onInflatedObjectContent(oh.Hash, oh.Offset, oh.Crc32, nil); err != nil {
		return err
	}

	return nil
}

func (p *Parser) resetCache(qty int) {
	if p.cache != nil {
		p.cache.Reset(qty)
	}
}

// Parse start decoding phase of the packfile.
func (p *Parser) Parse() (plumbing.Hash, error) {
	p.m.Lock()
	defer p.m.Unlock()

	var pendingDeltas []*ObjectHeader
	var pendingDeltaREFs []*ObjectHeader

	for p.scanner.Scan() {
		data := p.scanner.Data()
		switch data.Section {
		case HeaderSection:
			header := data.Value().(Header)

			p.resetCache(int(header.ObjectsQty))
			p.onHeader(header.ObjectsQty)

		case ObjectSection:
			oh := data.Value().(ObjectHeader)
			if oh.Type.IsDelta() {
				if oh.Type == plumbing.OFSDeltaObject {
					pendingDeltas = append(pendingDeltas, &oh)
				} else if oh.Type == plumbing.REFDeltaObject {
					pendingDeltaREFs = append(pendingDeltaREFs, &oh)
				}
				continue
			} else {
				if p.lowMemory && oh.content != nil {
					sync.PutBytesBuffer(oh.content)
					oh.content = nil
				}
				p.storeOrCache(&oh)
			}

		case FooterSection:
			p.checksum = data.Value().(plumbing.Hash)
		}
	}

	if p.scanner.objects == 0 {
		return plumbing.ZeroHash, ErrEmptyPackfile
	}

	for _, oh := range pendingDeltaREFs {
		err := p.processDelta(oh)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("processing ref-delta at offset %v: %w", oh.Offset, err)
		}
	}

	for _, oh := range pendingDeltas {
		err := p.processDelta(oh)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("processing ofs-delta at offset %v: %w", oh.Offset, err)
		}
	}

	return p.checksum, p.onFooter(p.checksum)
}

func (p *Parser) ensureContent(oh *ObjectHeader) error {
	// Skip if this object already has the correct content.
	if oh.content != nil && oh.content.Len() == int(oh.Size) {
		return nil
	}

	// if there is no parent, or it is not a delta object,
	// there is nothing to be done, as we can simply inflate
	// the object by its offset.
	if oh.parent != nil && oh.parent.diskType.IsDelta() {
		// for delta objects, if their content is empty or their size
		// is not correct, ensure that its contents, and its parents,
		// have their content and it is correctly inflated.
		//
		// Note that a drift between size and content size indicates that
		// the object may have been previously hydrated, and the content
		// may have later been overwritten by its compressed version.
		if oh.parent.content == nil || oh.parent.content.Len() != int(oh.parent.Size) {
			err := p.ensureContent(oh.parent)
			if err != nil {
				return fmt.Errorf("ensure parent content at offset %v: %w", oh.Offset, err)
			}
		}
	}

	deltaData := sync.GetBytesBuffer()
	defer sync.PutBytesBuffer(deltaData)

	err := p.scanner.inflateContent(oh.ContentOffset, deltaData)
	if err != nil {
		return fmt.Errorf("inflating content at offset %v: %w", oh.ContentOffset, err)
	}

	if oh.content == nil {
		oh.content = sync.GetBytesBuffer()
	}

	err = p.applyPatchBaseHeader(oh, deltaData, oh.content, nil)
	if err != nil {
		return fmt.Errorf("apply delta patch: %w", err)
	}
	return nil
}

func (p *Parser) processDelta(oh *ObjectHeader) error {
	switch oh.Type {
	case plumbing.OFSDeltaObject:
		pa, ok := p.cache.oiByOffset[oh.OffsetReference]
		if !ok {
			return plumbing.ErrObjectNotFound
		}
		oh.parent = pa

	case plumbing.REFDeltaObject:
		pa, ok := p.cache.oiByHash[oh.Reference]
		if !ok {
			// can't find referenced object in this pack file
			// this must be a "thin" pack.
			oh.parent = &ObjectHeader{ // Placeholder parent
				Hash:        oh.Reference,
				externalRef: true, // mark as an external reference that must be resolved
				Type:        plumbing.AnyObject,
				diskType:    plumbing.AnyObject,
			}
		} else {
			oh.parent = pa
		}
		p.cache.oiByHash[oh.Reference] = oh.parent

	default:
		return fmt.Errorf("unsupported delta type: %v", oh.Type)
	}

	if err := p.ensureContent(oh); err != nil {
		return err
	}

	return p.storeOrCache(oh)
}

// parentReader returns a [io.ReaderAt] for the decompressed contents
// of the parent.
func (p *Parser) parentReader(parent *ObjectHeader) (io.ReaderAt, error) {
	if parent.content != nil && parent.content.Len() > 0 {
		return bytes.NewReader(parent.content.Bytes()), nil
	}

	// If parent is a Delta object, the inflated object must come
	// from either cache or storage, else we would need to inflate
	// it to then inflate the current object, which could go on
	// indefinitely.
	if p.storage != nil && parent.Hash != plumbing.ZeroHash {
		obj, err := p.storage.EncodedObject(parent.Type, parent.Hash)
		if err == nil {
			// Ensure that external references have the correct type and size.
			parent.Type = obj.Type()
			parent.Size = obj.Size()
			r, err := obj.Reader()
			if err == nil {
				if parent.content == nil {
					parent.content = sync.GetBytesBuffer()
				}
				parent.content.Grow(int(parent.Size))

				_, err = ioutil.Copy(parent.content, r)
				if err == nil {
					return bytes.NewReader(parent.content.Bytes()), nil
				}
			}

			if err = r.Close(); err != nil {
				return nil, fmt.Errorf("close parent obj reader: %w", err)
			}
		}
	}

	// If the parent is not an external ref and we don't have the
	// content offset, we won't be able to inflate via seeking through
	// the packfile.
	if !parent.externalRef && parent.ContentOffset == 0 {
		return nil, plumbing.ErrObjectNotFound
	}

	// Not a seeker data source, so avoid seeking the content.
	if p.scanner.seeker == nil {
		return nil, plumbing.ErrObjectNotFound
	}

	if parent.content == nil {
		parent.content = sync.GetBytesBuffer()
	}
	parent.content.Grow(int(parent.Size))

	err := p.scanner.inflateContent(parent.ContentOffset, parent.content)
	if err != nil {
		return nil, ErrReferenceDeltaNotFound
	}
	return bytes.NewReader(parent.content.Bytes()), nil
}

func (p *Parser) applyPatchBaseHeader(ota *ObjectHeader, delta io.Reader, target io.Writer, wh objectHeaderWriter) error {
	if target == nil {
		return fmt.Errorf("cannot apply patch against nil target")
	}

	parentContents, err := p.parentReader(ota.parent)
	if err != nil {
		return err
	}

	typ := ota.Type
	if ota.Hash == plumbing.ZeroHash {
		typ = ota.parent.Type
	}

	sz, h, err := patchDeltaWriter(target, parentContents, delta, typ, wh)
	if err != nil {
		return err
	}

	if ota.Hash == plumbing.ZeroHash {
		ota.Type = typ
		ota.Size = int64(sz)
		ota.Hash = h
	}

	return nil
}

func (p *Parser) forEachObserver(f func(o Observer) error) error {
	for _, o := range p.observers {
		if err := f(o); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) onHeader(count uint32) error {
	return p.forEachObserver(func(o Observer) error {
		return o.OnHeader(count)
	})
}

func (p *Parser) onInflatedObjectHeader(
	t plumbing.ObjectType,
	objSize int64,
	pos int64,
) error {
	return p.forEachObserver(func(o Observer) error {
		return o.OnInflatedObjectHeader(t, objSize, pos)
	})
}

func (p *Parser) onInflatedObjectContent(
	h plumbing.Hash,
	pos int64,
	crc uint32,
	content []byte,
) error {
	return p.forEachObserver(func(o Observer) error {
		return o.OnInflatedObjectContent(h, pos, crc, content)
	})
}

func (p *Parser) onFooter(h plumbing.Hash) error {
	return p.forEachObserver(func(o Observer) error {
		return o.OnFooter(h)
	})
}
