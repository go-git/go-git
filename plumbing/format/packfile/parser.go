package packfile

import (
	"bytes"
	"context"
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

	// ErrParserConsumed is returned by Parse when called against a Parser
	// instance that has already been consumed by a prior Parse call,
	// whether that call returned successfully or with an error. Parsers
	// are single-shot; construct a new one per pack.
	ErrParserConsumed = errors.New("parser already consumed")
)

// maxObjectPreallocBytes caps the up-front size hint passed to
// bytes.Buffer.Grow when staging an object's contents, so a malformed length
// cannot trigger a huge or out-of-range allocation. The buffer still grows
// dynamically as data is written; this is purely a hint cap.
const maxObjectPreallocBytes = 1 << 30 // 1 GiB

// Match upstream Git's pack depth ceiling: pack-objects.h OE_DEPTH_BITS,
// enforced in builtin/pack-objects.c as (1 << OE_DEPTH_BITS) - 1.
const maxDeltaChainDepth = 4095

// growHint returns a non-negative int64 size, clamped to a sane upper bound,
// suitable for passing to bytes.Buffer.Grow.
func growHint(n int64) int {
	switch {
	case n <= 0:
		return 0
	case n > maxObjectPreallocBytes:
		return maxObjectPreallocBytes
	default:
		return int(n)
	}
}

// Parser decodes a packfile and calls any observer associated to it. Is used
// to generate indexes.
//
// A Parser is single-shot: Parse may be called at most once per
// instance. The cache maps and the per-delta parent pointers built up
// during a Parse call are not reset on entry, so a second call would
// observe the prior call's state — successful or not — and produce
// undefined results; the second call therefore returns
// ErrParserConsumed without running. Construct a new Parser for each
// pack you intend to decode.
type Parser struct {
	storage       storer.EncodedObjectStorer
	cache         *parserCache
	lowMemoryMode bool

	scanner   *Scanner
	observers []Observer
	hasher    plumbing.Hasher

	objectFormat format.ObjectFormat

	checksum plumbing.Hash
	m        stdsync.Mutex
	parsed   bool
}

// LowMemoryCapable is implemented by storage types that are capable of
// operating in low-memory mode.
type LowMemoryCapable interface {
	// LowMemoryMode defines whether the storage is able and willing for
	// the parser to operate in low-memory mode.
	LowMemoryMode() bool
}

// NewParser creates a new Parser.
// When a storage is set, the objects are written to storage as they
// are parsed.
func NewParser(data io.Reader, opts ...ParserOption) *Parser {
	p := &Parser{
		objectFormat: format.DefaultObjectFormat,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}

	p.hasher = plumbing.NewHasher(p.objectFormat, plumbing.AnyObject, 0)
	var sopts []ScannerOption
	if p.objectFormat == format.SHA256 {
		sopts = append(sopts, WithSHA256())
	}

	p.scanner = NewScanner(data, sopts...)

	if p.storage != nil {
		p.scanner.storage = p.storage

		lm, ok := p.storage.(LowMemoryCapable)
		p.lowMemoryMode = ok && lm.LowMemoryMode()
	}

	if p.scanner.seeker == nil {
		p.lowMemoryMode = false
	}
	p.scanner.lowMemoryMode = p.lowMemoryMode
	p.cache = newParserCache()

	return p
}

func (p *Parser) storeOrCache(ctx context.Context, oh *ObjectHeader) error {
	// Only need to store deltas, as the scanner already stored non-delta
	// objects.
	if p.storage != nil && oh.diskType.IsDelta() {
		w, err := p.storage.RawObjectWriter(ctx, oh.Type, oh.Size)
		if err != nil {
			return err
		}

		defer func() { _ = w.Close() }()

		_, err = ioutil.CopyBufferPool(w, oh.content)
		if err != nil {
			return err
		}
	}

	if p.cache != nil {
		o := oh
		for p.lowMemoryMode && o.content != nil {
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

	return p.onInflatedObjectContent(oh.Hash, oh.Offset, oh.Crc32, nil)
}

func (p *Parser) resetCache(qty int) {
	if p.cache != nil {
		p.cache.Reset(qty)
	}
}

// Parse start decoding phase of the packfile.
func (p *Parser) Parse(ctx context.Context) (plumbing.Hash, error) {
	p.m.Lock()
	defer p.m.Unlock()

	if p.parsed {
		return plumbing.ZeroHash, ErrParserConsumed
	}
	p.parsed = true
	p.scanner.ctx = ctx

	var pendingDeltas []*ObjectHeader
	var pendingDeltaREFs []*ObjectHeader

	for p.scanner.Scan() {
		data := p.scanner.Data()
		switch data.Section {
		case HeaderSection:
			header := data.Value().(Header)

			p.resetCache(int(header.ObjectsQty))
			_ = p.onHeader(header.ObjectsQty)

		case ObjectSection:
			oh := data.Value().(ObjectHeader)
			if oh.Type.IsDelta() {
				oh.Hash.ResetBySize(p.scanner.objectIDSize)
				switch oh.Type {
				case plumbing.OFSDeltaObject:
					pendingDeltas = append(pendingDeltas, &oh)
				case plumbing.REFDeltaObject:
					pendingDeltaREFs = append(pendingDeltaREFs, &oh)
				}
				continue
			}

			if p.lowMemoryMode && oh.content != nil {
				sync.PutBytesBuffer(oh.content)
				oh.content = nil
			}

			_ = p.storeOrCache(ctx, &oh)

		case FooterSection:
			p.checksum = data.Value().(plumbing.Hash)
		}
	}

	err := p.scanner.Error()
	if err != nil {
		if errors.Is(err, io.EOF) && p.scanner.objects == 0 {
			return plumbing.ZeroHash, ErrEmptyPackfile
		}
		return plumbing.ZeroHash, err
	}

	if err := p.resolveDeltas(ctx, pendingDeltas, pendingDeltaREFs); err != nil {
		return plumbing.ZeroHash, err
	}

	// Return to pool all objects used.
	go func() {
		for _, oh := range p.cache.oi {
			if oh.content != nil {
				sync.PutBytesBuffer(oh.content)
				oh.content = nil
			}
		}
	}()

	return p.checksum, p.onFooter(p.checksum)
}

func (p *Parser) ensureContent(ctx context.Context, oh *ObjectHeader) error {
	// Skip if this object already has the correct content.
	if oh.content != nil && oh.content.Len() == int(oh.Size) && !oh.Hash.IsZero() {
		return nil
	}

	if oh.content == nil {
		oh.content = sync.GetBytesBuffer()
	}

	var err error
	switch {
	case !p.lowMemoryMode && oh.content != nil && oh.content.Len() > 0:
		source := oh.content
		oh.content = sync.GetBytesBuffer()

		defer sync.PutBytesBuffer(source)

		err = p.applyPatchBaseHeader(ctx, oh, source, oh.content, nil)
	case p.scanner.seeker != nil:
		deltaData := sync.GetBytesBuffer()
		defer sync.PutBytesBuffer(deltaData)

		err = p.scanner.inflateContent(oh.ContentOffset, deltaData, oh.Size)
		if err != nil {
			return fmt.Errorf("inflating content at offset %v: %w", oh.ContentOffset, err)
		}

		err = p.applyPatchBaseHeader(ctx, oh, deltaData, oh.content, nil)
	default:
		return fmt.Errorf("can't ensure content: %w", plumbing.ErrObjectNotFound)
	}

	if err != nil {
		return fmt.Errorf("apply delta patch: %w", err)
	}
	return nil
}

// resolveDeltas walks the pack's delta DAG depth-first from each
// non-delta base, processing OFS and REF delta children of every parent
// together. Mirrors canonical Git's threaded_second_pass in
// builtin/index-pack.c[1], which advances both kinds of children from
// each in-progress parent in a single walk.
//
// Splitting REF and OFS resolution into separate passes (REF first, OFS
// second) is incorrect: a REF-delta whose base is an OFS-delta in the
// same pack would look up its base hash before the OFS-delta has been
// applied, since the OFS-delta's resolved hash is unknown at scan time.
// The lookup would then misclassify the in-pack base as a thin-pack
// external reference and the chain would fail to resolve.
//
// Any REF-delta not reached through the depth-first walk has a base
// outside this pack and is processed via the external-reference
// placeholder path. An OFS-delta whose recorded negative offset does
// not match any in-pack object header is rejected as malformed input.
//
// [1]: https://github.com/git/git/blob/v2.54.0/builtin/index-pack.c#L1103
func (p *Parser) resolveDeltas(ctx context.Context, ofsDeltas, refDeltas []*ObjectHeader) error {
	// Map sizes correspond to the count of distinct parent offsets /
	// hashes, not the count of delta entries. Real packs cluster many
	// children under one parent (chains and wide trees), so a hint
	// sized to len(deltas) consistently overshoots. Let the maps grow.
	ofsChildren := map[int64][]*ObjectHeader{}
	for _, d := range ofsDeltas {
		ofsChildren[d.OffsetReference] = append(ofsChildren[d.OffsetReference], d)
	}
	refChildren := map[plumbing.Hash][]*ObjectHeader{}
	for _, d := range refDeltas {
		refChildren[d.Reference] = append(refChildren[d.Reference], d)
	}

	var visit func(*ObjectHeader) error
	visit = func(parent *ObjectHeader) error {
		for _, c := range refChildren[parent.Hash] {
			// Two non-delta entries with identical content (or an
			// OFS-delta that resolves to the same hash as a non-delta
			// elsewhere in the pack) make this child reachable from
			// more than one parent; only the first reach resolves it.
			if c.parent != nil {
				continue
			}
			if err := p.processDelta(ctx, c); err != nil {
				return fmt.Errorf("processing ref-delta at offset %v: %w", c.Offset, err)
			}
			if err := visit(c); err != nil {
				return err
			}
		}
		for _, c := range ofsChildren[parent.Offset] {
			if c.parent != nil {
				continue
			}
			if err := p.processDelta(ctx, c); err != nil {
				return fmt.Errorf("processing ofs-delta at offset %v: %w", c.Offset, err)
			}
			if err := visit(c); err != nil {
				return err
			}
		}
		return nil
	}

	// Snapshot the non-delta bases before walking, since processDelta
	// appends resolved deltas to p.cache.oi via storeOrCache. The
	// non-delta fraction of a real pack is small (typical 5-20%), so
	// preallocating to len(p.cache.oi) would waste most of the slot.
	var bases []*ObjectHeader
	for _, oh := range p.cache.oi {
		if !oh.Type.IsDelta() {
			bases = append(bases, oh)
		}
	}
	for _, base := range bases {
		if err := visit(base); err != nil {
			return err
		}
	}

	for _, d := range refDeltas {
		if d.parent != nil {
			continue
		}
		if err := p.processDelta(ctx, d); err != nil {
			return fmt.Errorf("processing ref-delta at offset %v: %w", d.Offset, err)
		}
	}

	for _, d := range ofsDeltas {
		if d.parent != nil {
			continue
		}
		return fmt.Errorf("processing ofs-delta at offset %v: %w", d.Offset, plumbing.ErrObjectNotFound)
	}

	return nil
}

func (p *Parser) processDelta(ctx context.Context, oh *ObjectHeader) error {
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
		// For a thin-pack external reference, store the placeholder so
		// subsequent REF-deltas naming the same external hash chain
		// through this entry. For an in-pack base the write is a no-op.
		p.cache.oiByHash[oh.Reference] = oh.parent

	default:
		return fmt.Errorf("unsupported delta type: %v", oh.Type)
	}

	if err := checkDeltaChainDepth(oh); err != nil {
		return err
	}

	if err := p.ensureContent(ctx, oh); err != nil {
		return err
	}

	return p.storeOrCache(ctx, oh)
}

// checkDeltaChainDepth verifies that the delta chain rooted at oh
// stays within [maxDeltaChainDepth] links. The result is cached on
// [ObjectHeader.chainDepth] so a subsequent walk that crosses the
// same parent reuses the work — every entry on the chain ends up
// with its depth set once, which keeps the verification linear in
// the number of distinct objects rather than quadratic in the
// chain length. This mirrors the cached `oe->depth` field that
// upstream Git carries on the object entry in
// `builtin/pack-objects.c`.
func checkDeltaChainDepth(oh *ObjectHeader) error {
	if oh.chainDepth > 0 {
		return nil
	}
	var depth int
	for current := oh; current != nil && current.isDeltaOnDisk(); current = current.parent {
		if current.chainDepth > 0 {
			depth += current.chainDepth
			if depth > maxDeltaChainDepth {
				return fmt.Errorf("%w: delta chain depth exceeds %d", ErrMalformedPackfile, maxDeltaChainDepth)
			}
			break
		}
		depth++
		if depth > maxDeltaChainDepth {
			return fmt.Errorf("%w: delta chain depth exceeds %d", ErrMalformedPackfile, maxDeltaChainDepth)
		}
	}
	oh.chainDepth = depth
	return nil
}

func (oh *ObjectHeader) isDeltaOnDisk() bool {
	return oh.Type.IsDelta() || oh.diskType.IsDelta()
}

// parentReader returns a [io.ReaderAt] for the decompressed contents
// of the parent.
func (p *Parser) parentReader(ctx context.Context, parent *ObjectHeader) (io.ReaderAt, error) {
	if parent.content != nil && parent.content.Len() > 0 {
		return bytes.NewReader(parent.content.Bytes()), nil
	}

	// If parent is a Delta object, the inflated object must come
	// from either cache or storage, else we would need to inflate
	// it to then inflate the current object, which could go on
	// indefinitely.
	if p.storage != nil && parent.Hash != plumbing.ZeroHash {
		obj, err := p.storage.EncodedObject(ctx, parent.Type, parent.Hash)
		if err == nil {
			// Ensure that external references have the correct type and size.
			parent.Type = obj.Type()
			parent.Size = obj.Size()
			r, err := obj.Reader()
			if err == nil {
				defer func() { _ = r.Close() }()

				if parent.content == nil {
					parent.content = sync.GetBytesBuffer()
				}
				parent.content.Grow(growHint(parent.Size))

				_, err = ioutil.CopyBufferPool(parent.content, r)
				if err == nil {
					return bytes.NewReader(parent.content.Bytes()), nil
				}
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
	parent.content.Grow(growHint(parent.Size))

	err := p.scanner.inflateContent(parent.ContentOffset, parent.content, parent.Size)
	if err != nil {
		return nil, ErrReferenceDeltaNotFound
	}
	return bytes.NewReader(parent.content.Bytes()), nil
}

func (p *Parser) applyPatchBaseHeader(ctx context.Context, ota *ObjectHeader, delta io.Reader, target io.Writer, wh objectHeaderWriter) error {
	if target == nil {
		return fmt.Errorf("cannot apply patch against nil target")
	}

	parentContents, err := p.parentReader(ctx, ota.parent)
	if err != nil {
		return err
	}

	typ := ota.Type
	if ota.Hash.IsZero() {
		typ = ota.parent.Type
	}

	sz, h, err := patchDeltaWriter(target, parentContents, delta, typ, wh, p.objectFormat)
	if err != nil {
		return err
	}

	if ota.Hash.IsZero() {
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
