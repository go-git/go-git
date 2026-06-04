package index

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
	"github.com/go-git/go-git/v6/utils/trace"
)

var (
	// DecodeVersionSupported is the range of supported index versions.
	DecodeVersionSupported = struct{ Min, Max uint32 }{Min: 2, Max: 4}

	// ErrMalformedSignature is returned by Decode when the index header file is
	// malformed.
	ErrMalformedSignature = errors.New("index decoder: malformed index signature file")
	// ErrInvalidChecksum is returned by Decode if the SHA1/SHA256 hash mismatch with
	// the read content.
	ErrInvalidChecksum = errors.New("index decoder: invalid checksum")
	// ErrUnknownExtension is returned when an index extension is encountered that is considered mandatory.
	ErrUnknownExtension = errors.New("index decoder: unknown extension")
	// ErrMalformedIndexFile is returned when the index file contents are
	// structurally invalid.
	ErrMalformedIndexFile = errors.New("index decoder: malformed index file")
)

const (
	entryHeaderLength = 42
	entryExtended     = 0x4000
	nameMask          = 0xfff
	intentToAddMask   = 1 << 13
	skipWorkTreeMask  = 1 << 14
)

// A Decoder reads and decodes index files from an input stream.
type Decoder struct {
	buf       *bufio.Reader
	r         io.Reader
	hash      hash.Hash
	lastEntry *Entry
	skipHash  bool

	extReader *bufio.Reader
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader, h hash.Hash, opts ...Option) *Decoder {
	var cfg options
	for _, o := range opts {
		o(&cfg)
	}

	buf := bufio.NewReader(r)
	d := &Decoder{
		buf:       buf,
		hash:      h,
		skipHash:  cfg.skipHash,
		extReader: bufio.NewReader(nil),
	}

	if d.skipHash {
		d.r = buf
	} else {
		h.Reset()
		d.r = io.TeeReader(buf, h)
	}

	return d
}

// Decode reads the whole index object from its input and stores it in the
// value pointed to by idx.
func (d *Decoder) Decode(idx *Index) error {
	var err error
	idx.Version, err = validateHeader(d.r)
	if err != nil {
		return err
	}

	trace.Internal.Printf("index: decode version %d", idx.Version)

	entryCount, err := binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	trace.Internal.Printf("index: decode entry count %d", entryCount)

	if err := d.readEntries(idx, int(entryCount)); err != nil {
		return err
	}

	return d.readExtensions(idx)
}

func (d *Decoder) readEntries(idx *Index, count int) error {
	for range count {
		e, err := d.readEntry(idx)
		if err != nil {
			return err
		}

		d.lastEntry = e
		idx.Entries = append(idx.Entries, e)
	}

	return nil
}

func (d *Decoder) readEntry(idx *Index) (*Entry, error) {
	e := &Entry{}

	var msec, mnsec, sec, nsec uint32
	var flags uint16

	flow := []any{
		&sec, &nsec,
		&msec, &mnsec,
		&e.Dev,
		&e.Inode,
		&e.Mode,
		&e.UID,
		&e.GID,
		&e.Size,
	}

	if err := binary.Read(d.r, flow...); err != nil {
		return nil, err
	}

	e.Hash.ResetBySize(d.hash.Size())
	if _, err := e.Hash.ReadFrom(d.r); err != nil {
		return nil, err
	}

	if err := binary.Read(d.r, &flags); err != nil {
		return nil, err
	}

	read := entryHeaderLength + d.hash.Size()

	if sec != 0 || nsec != 0 {
		e.CreatedAt = time.Unix(int64(sec), int64(nsec))
	}

	if msec != 0 || mnsec != 0 {
		e.ModifiedAt = time.Unix(int64(msec), int64(mnsec))
	}

	e.Stage = Stage(flags>>12) & 0x3

	if flags&entryExtended != 0 {
		extended, err := binary.ReadUint16(d.r)
		if err != nil {
			return nil, err
		}

		read += 2
		e.IntentToAdd = extended&intentToAddMask != 0
		e.SkipWorktree = extended&skipWorkTreeMask != 0
	}

	nameConsumed, err := d.readEntryName(idx, e, flags)
	if err != nil {
		return nil, err
	}

	return e, d.padEntry(idx, e, read, nameConsumed)
}

// readEntryName reads the entry path and sets e.Name. It returns the
// number of bytes consumed from the stream for the name portion.
func (d *Decoder) readEntryName(idx *Index, e *Entry, flags uint16) (int, error) {
	switch idx.Version {
	case 2, 3:
		nameLen := flags & nameMask
		name, consumed, err := d.doReadEntryName(nameLen)
		if err != nil {
			return 0, err
		}
		e.Name = name
		return consumed, nil
	case 4:
		name, err := d.doReadEntryNameV4()
		if err != nil {
			return 0, err
		}
		e.Name = name
		return 0, nil // V4 has no padding; consumed count unused
	default:
		return 0, ErrUnsupportedVersion
	}
}

// doReadEntryName reads the entry path for V2/V3 indexes. It returns the
// name, the number of bytes consumed from the stream, and any error.
// When nameLen equals nameMask (0xFFF), the name was too long to fit in
// the 12-bit field and the real length is found by scanning for the NUL
// terminator — matching C Git's strlen(name) fallback in create_from_disk.
func (d *Decoder) doReadEntryName(nameLen uint16) (string, int, error) {
	if nameLen == nameMask {
		name, err := binary.ReadUntil(d.r, '\x00')
		if err != nil {
			return "", 0, err
		}
		return string(name), len(name) + 1, nil // +1 for the consumed NUL delimiter
	}

	name := make([]byte, nameLen)
	_, err := io.ReadFull(d.r, name)
	return string(name), int(nameLen), err
}

func (d *Decoder) doReadEntryNameV4() (string, error) {
	l, err := binary.ReadVariableWidthInt(d.r)
	if err != nil {
		return "", err
	}

	var base string
	if d.lastEntry != nil {
		if l < 0 || int(l) > len(d.lastEntry.Name) {
			return "", fmt.Errorf("%w: invalid V4 entry name strip length %d (previous name length: %d)",
				ErrMalformedIndexFile, l, len(d.lastEntry.Name))
		}
		base = d.lastEntry.Name[:len(d.lastEntry.Name)-int(l)]
	} else if l > 0 {
		return "", fmt.Errorf("%w: non-zero strip length %d on first V4 entry",
			ErrMalformedIndexFile, l)
	}

	name, err := binary.ReadUntil(d.r, '\x00')
	if err != nil {
		return "", err
	}

	return base + string(name), nil
}

// padEntry discards NUL padding bytes that follow each V2/V3 entry on
// disk. nameConsumed is the number of stream bytes consumed while reading
// the entry name (which may exceed len(e.Name) when a NUL terminator was
// consumed for long names where the 12-bit length field overflowed).
func (d *Decoder) padEntry(idx *Index, e *Entry, read, nameConsumed int) error {
	if idx.Version == 4 {
		return nil
	}

	entrySize := read + len(e.Name)
	padLen := 8 - entrySize%8
	padLen -= nameConsumed - len(e.Name)
	if padLen > 0 {
		_, err := io.CopyN(io.Discard, d.r, int64(padLen))
		return err
	}
	return nil
}

func (d *Decoder) readExtensions(idx *Index) error {
	// TODO: support 'Split index' and 'Untracked cache' extensions, take in
	// count that they are not supported by jgit or libgit

	var expected []byte
	var peeked []byte
	var err error

	// we should always be able to peek for 4 bytes (header) + 4 bytes (extlen) + final hash
	// if this fails, we know that we're at the end of the index
	peekLen := 4 + 4 + d.hash.Size()

	for {
		if !d.skipHash {
			expected = d.hash.Sum(nil)
		}
		peeked, err = d.buf.Peek(peekLen)
		if len(peeked) < peekLen {
			trace.Internal.Printf("index: decode peeked %d bytes, less than minimum %d; done reading extensions", len(peeked), peekLen)
			// there can't be an extension at this point, so let's bail out
			break
		}
		if err != nil {
			return err
		}

		err = d.readExtension(idx)
		if err != nil {
			return err
		}
	}

	if !d.skipHash {
		trace.Internal.Printf("index: verifying checksum, expected %x", expected)
	}
	return d.readChecksum(expected)
}

func (d *Decoder) readExtension(idx *Index) error {
	var header [4]byte

	if _, err := io.ReadFull(d.r, header[:]); err != nil {
		return err
	}

	trace.Internal.Printf("index: decode extension header %s", string(header[:]))

	r, err := d.getExtensionReader()
	if err != nil {
		return err
	}

	switch {
	case bytes.Equal(header[:], treeExtSignature):
		trace.Internal.Printf("index: decoding tree extension")
		idx.Cache = &Tree{}
		extDec := &treeExtensionDecoder{r, d.hash}
		if err := extDec.Decode(idx.Cache); err != nil {
			return err
		}
		trace.Internal.Printf("index: tree extension decoded, %d entries", len(idx.Cache.Entries))
	case bytes.Equal(header[:], resolveUndoExtSignature):
		trace.Internal.Printf("index: decoding resolve-undo extension")
		idx.ResolveUndo = &ResolveUndo{}
		extDec := &resolveUndoDecoder{r, d.hash}
		if err := extDec.Decode(idx.ResolveUndo); err != nil {
			return err
		}
		trace.Internal.Printf("index: resolve-undo extension decoded, %d entries", len(idx.ResolveUndo.Entries))
	case bytes.Equal(header[:], endOfIndexEntryExtSignature):
		trace.Internal.Printf("index: decoding end-of-index-entry extension")
		idx.EndOfIndexEntry = &EndOfIndexEntry{}
		extDec := &endOfIndexEntryDecoder{r, d.hash}
		if err := extDec.Decode(idx.EndOfIndexEntry); err != nil {
			return err
		}
		trace.Internal.Printf("index: end-of-index-entry extension decoded, offset %d hash %s", idx.EndOfIndexEntry.Offset, idx.EndOfIndexEntry.Hash)
	default:
		// See https://git-scm.com/docs/index-format, which says:
		// If the first byte is 'A'..'Z' the extension is optional and can be ignored.
		if header[0] < 'A' || header[0] > 'Z' {
			trace.Internal.Printf("index: unknown mandatory extension %s", string(header[:]))
			return ErrUnknownExtension
		}

		trace.Internal.Printf("index: skipping optional unknown extension %s", string(header[:]))
		extDec := &unknownExtensionDecoder{r}
		if err := extDec.Decode(); err != nil {
			return err
		}
	}

	return nil
}

func (d *Decoder) getExtensionReader() (*bufio.Reader, error) {
	extLen, err := binary.ReadUint32(d.r)
	if err != nil {
		return nil, err
	}

	d.extReader.Reset(&io.LimitedReader{R: d.r, N: int64(extLen)})
	return d.extReader, nil
}

func (d *Decoder) readChecksum(expected []byte) error {
	var h plumbing.Hash
	h.ResetBySize(d.hash.Size())

	if _, err := h.ReadFrom(d.r); err != nil {
		trace.Internal.Printf("index: checksum read error: %v", err)
		return err
	}

	if d.skipHash {
		trace.Internal.Printf("index: skipping checksum verification (skipHash)")
		return nil
	}

	if h.Compare(expected) != 0 {
		trace.Internal.Printf("index: checksum mismatch, expected %x got %s", expected, h)
		return ErrInvalidChecksum
	}

	trace.Internal.Printf("index: checksum ok %s", h)
	return nil
}

func validateHeader(r io.Reader) (version uint32, err error) {
	s := make([]byte, 4)
	if _, err := io.ReadFull(r, s); err != nil {
		return 0, err
	}

	if !bytes.Equal(s, indexSignature) {
		return 0, ErrMalformedSignature
	}

	version, err = binary.ReadUint32(r)
	if err != nil {
		return 0, err
	}

	if version < DecodeVersionSupported.Min || version > DecodeVersionSupported.Max {
		return 0, ErrUnsupportedVersion
	}

	return version, err
}

type treeExtensionDecoder struct {
	r *bufio.Reader
	h hash.Hash
}

func (d *treeExtensionDecoder) Decode(t *Tree) error {
	for {
		e, err := d.readEntry()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		if e == nil {
			continue
		}

		t.Entries = append(t.Entries, *e)
	}
}

func (d *treeExtensionDecoder) readEntry() (*TreeEntry, error) {
	e := &TreeEntry{}

	path, err := binary.ReadUntil(d.r, '\x00')
	if err != nil {
		return nil, err
	}

	e.Path = string(path)

	count, err := binary.ReadUntil(d.r, ' ')
	if err != nil {
		return nil, err
	}

	i, err := strconv.Atoi(string(count))
	if err != nil {
		return nil, err
	}

	e.Entries = i
	trees, err := binary.ReadUntil(d.r, '\n')
	if err != nil {
		return nil, err
	}

	subtrees, err := strconv.Atoi(string(trees))
	if err != nil {
		return nil, err
	}

	e.Trees = subtrees

	// An entry can be in an invalidated state and is represented by having a
	// negative number in the entry_count field. In this case, there is no
	// object name and the next entry starts immediately after the newline.
	if i < 0 {
		trace.Internal.Printf("index: tree extension entry %q invalidated (entry count %d)", e.Path, i)
		return nil, nil
	}

	e.Hash.ResetBySize(d.h.Size())
	_, err = e.Hash.ReadFrom(d.r)
	if err != nil {
		return nil, err
	}
	return e, nil
}

type resolveUndoDecoder struct {
	r *bufio.Reader
	h hash.Hash
}

func (d *resolveUndoDecoder) Decode(ru *ResolveUndo) error {
	for {
		e, err := d.readEntry()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		ru.Entries = append(ru.Entries, *e)
	}
}

func (d *resolveUndoDecoder) readEntry() (*ResolveUndoEntry, error) {
	e := &ResolveUndoEntry{
		Stages: make(map[Stage]plumbing.Hash),
	}

	path, err := binary.ReadUntil(d.r, '\x00')
	if err != nil {
		return nil, err
	}

	e.Path = string(path)

	for i := range 3 {
		if err := d.readStage(e, Stage(i+1)); err != nil {
			return nil, err
		}
	}

	for s := range e.Stages {
		var h plumbing.Hash
		h.ResetBySize(d.h.Size())
		if _, err := h.ReadFrom(d.r); err != nil {
			return nil, err
		}

		e.Stages[s] = h
	}

	trace.Internal.Printf("index: resolve-undo entry %q, %d stages", e.Path, len(e.Stages))
	return e, nil
}

func (d *resolveUndoDecoder) readStage(e *ResolveUndoEntry, s Stage) error {
	ascii, err := binary.ReadUntil(d.r, '\x00')
	if err != nil {
		return err
	}

	stage, err := strconv.ParseInt(string(ascii), 8, 64)
	if err != nil {
		return err
	}

	if stage != 0 {
		e.Stages[s] = plumbing.ZeroHash
	}

	return nil
}

type endOfIndexEntryDecoder struct {
	r *bufio.Reader
	h hash.Hash
}

func (d *endOfIndexEntryDecoder) Decode(e *EndOfIndexEntry) error {
	var err error
	e.Offset, err = binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	e.Hash.ResetBySize(d.h.Size())
	_, err = e.Hash.ReadFrom(d.r)
	return err
}

type unknownExtensionDecoder struct {
	r *bufio.Reader
}

func (d *unknownExtensionDecoder) Decode() error {
	_, err := io.Copy(io.Discard, d.r)
	return err
}
