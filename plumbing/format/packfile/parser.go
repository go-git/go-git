package packfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	stdsync "sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
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
	storage storer.EncodedObjectStorer
	cache   *parserCache

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
		hasher: plumbing.NewHasher(plumbing.AnyObject, 0),
	}
	for _, opt := range opts {
		opt(p)
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

		_, err = io.Copy(w, bytes.NewReader(oh.content.Bytes()))
		if err != nil {
			return err
		}
	}

	if p.cache != nil {
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
			return plumbing.ZeroHash, err
		}
	}

	for _, oh := range pendingDeltas {
		err := p.processDelta(oh)
		if err != nil {
			return plumbing.ZeroHash, err
		}
	}

	return p.checksum, p.onFooter(p.checksum)
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
			oh.parent = &ObjectHeader{ //Placeholder parent
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

	parentContents, err := p.parentReader(oh.parent)
	if err != nil {
		return err
	}

	var deltaData bytes.Buffer
	if oh.content.Len() > 0 {
		_, err = oh.content.WriteTo(&deltaData)
		if err != nil {
			return err
		}
	} else {
		deltaData = *bytes.NewBuffer(make([]byte, 0, oh.Size))
		err = p.scanner.inflateContent(oh.ContentOffset, &deltaData)
		if err != nil {
			return err
		}
	}

	w, err := p.cacheWriter(oh)
	if err != nil {
		return err
	}

	defer w.Close()

	err = applyPatchBaseHeader(oh, parentContents, &deltaData, w, nil)
	if err != nil {
		return err
	}

	return p.storeOrCache(oh)
}

func (p *Parser) parentReader(parent *ObjectHeader) (io.ReaderAt, error) {
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
				parentData := bytes.NewBuffer(make([]byte, 0, parent.Size))

				_, err = io.Copy(parentData, r)
				r.Close()

				if err == nil {
					return bytes.NewReader(parentData.Bytes()), nil
				}
			}
		}
	}

	if p.cache != nil && parent.content.Len() > 0 {
		return bytes.NewReader(parent.content.Bytes()), nil
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

	parentData := bytes.NewBuffer(make([]byte, 0, parent.Size))
	err := p.scanner.inflateContent(parent.ContentOffset, parentData)
	if err != nil {
		return nil, ErrReferenceDeltaNotFound
	}
	return bytes.NewReader(parentData.Bytes()), nil
}

func (p *Parser) cacheWriter(oh *ObjectHeader) (io.WriteCloser, error) {
	return ioutil.NewWriteCloser(&oh.content, nil), nil
}

func applyPatchBaseHeader(ota *ObjectHeader, base io.ReaderAt, delta io.Reader, target io.Writer, wh objectHeaderWriter) error {
	if target == nil {
		return fmt.Errorf("cannot apply patch against nil target")
	}

	typ := ota.Type
	if ota.Hash == plumbing.ZeroHash {
		typ = ota.parent.Type
	}

	sz, h, err := patchDeltaWriter(target, base, delta, typ, wh)
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
