package index

import (
	"bufio"
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"io"

	"strconv"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/ewah"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
)

var (
	// DecodeVersionSupported is the range of supported index versions
	DecodeVersionSupported = struct{ Min, Max uint32 }{Min: 2, Max: 4}

	// ErrMalformedSignature is returned by Decode when the index header file is
	// malformed
	ErrMalformedSignature = errors.New("malformed index signature file")
	// ErrInvalidChecksum is returned by Decode if the SHA1 hash mismatch with
	// the read content
	ErrInvalidChecksum = errors.New("invalid checksum")
	// ErrUnknownExtension is returned when an index extension is encountered that is considered mandatory
	ErrUnknownExtension = errors.New("unknown extension")
)

const (
	entryHeaderLength = 62
	entryExtended     = 0x4000
	entryValid        = 0x8000
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

	extReader *bufio.Reader
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	// TODO: Support passing an ObjectFormat (sha256)
	h := hash.New(crypto.SHA1)
	buf := bufio.NewReader(r)
	return &Decoder{
		buf:       buf,
		r:         io.TeeReader(buf, h),
		hash:      h,
		extReader: bufio.NewReader(nil),
	}
}

// Decode reads the whole index object from its input and stores it in the
// value pointed to by idx.
func (d *Decoder) Decode(idx *Index) error {
	var err error
	idx.Version, err = validateHeader(d.r)
	if err != nil {
		return err
	}

	entryCount, err := binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	if err := d.readEntries(idx, int(entryCount)); err != nil {
		return err
	}

	return d.readExtensions(idx)
}

func (d *Decoder) readEntries(idx *Index, count int) error {
	for i := 0; i < count; i++ {
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

	flow := []interface{}{
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

	if _, err := e.Hash.ReadFrom(d.r); err != nil {
		return nil, err
	}

	if err := binary.Read(d.r, &flags); err != nil {
		return nil, err
	}

	read := entryHeaderLength

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

	if err := d.readEntryName(idx, e, flags); err != nil {
		return nil, err
	}

	return e, d.padEntry(idx, e, read)
}

func (d *Decoder) readEntryName(idx *Index, e *Entry, flags uint16) error {
	var name string
	var err error

	switch idx.Version {
	case 2, 3:
		len := flags & nameMask
		name, err = d.doReadEntryName(len)
	case 4:
		name, err = d.doReadEntryNameV4()
	default:
		return ErrUnsupportedVersion
	}

	if err != nil {
		return err
	}

	e.Name = name
	return nil
}

func (d *Decoder) doReadEntryNameV4() (string, error) {
	l, err := binary.ReadVariableWidthInt(d.r)
	if err != nil {
		return "", err
	}

	var base string
	if d.lastEntry != nil {
		base = d.lastEntry.Name[:len(d.lastEntry.Name)-int(l)]
	}

	name, err := binary.ReadUntil(d.r, '\x00')
	if err != nil {
		return "", err
	}

	return base + string(name), nil
}

func (d *Decoder) doReadEntryName(len uint16) (string, error) {
	name := make([]byte, len)
	_, err := io.ReadFull(d.r, name)

	return string(name), err
}

// Index entries are padded out to the next 8 byte alignment
// for historical reasons related to how C Git read the files.
func (d *Decoder) padEntry(idx *Index, e *Entry, read int) error {
	if idx.Version == 4 {
		return nil
	}

	entrySize := read + len(e.Name)
	padLen := 8 - entrySize%8
	_, err := io.CopyN(io.Discard, d.r, int64(padLen))
	return err
}

func (d *Decoder) readExtensions(idx *Index) error {
	var expected []byte
	var peeked []byte
	var err error

	// we should always be able to peek for 4 bytes (header) + 4 bytes (extlen) + final hash
	// if this fails, we know that we're at the end of the index
	peekLen := 4 + 4 + d.hash.Size()

	for {
		expected = d.hash.Sum(nil)
		peeked, err = d.buf.Peek(peekLen)
		if len(peeked) < peekLen {
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

	return d.readChecksum(expected)
}

func (d *Decoder) readExtension(idx *Index) error {
	var header [4]byte

	if _, err := io.ReadFull(d.r, header[:]); err != nil {
		return err
	}

	r, err := d.getExtensionReader()
	if err != nil {
		return err
	}

	switch {
	case bytes.Equal(header[:], treeExtSignature):
		idx.Cache = &Tree{}
		d := &treeExtensionDecoder{r}
		if err := d.Decode(idx.Cache); err != nil {
			return err
		}

	case bytes.Equal(header[:], resolveUndoExtSignature):
		idx.ResolveUndo = &ResolveUndo{}
		d := &resolveUndoDecoder{r}
		if err := d.Decode(idx.ResolveUndo); err != nil {
			return err
		}

	case bytes.Equal(header[:], endOfIndexEntryExtSignature):
		idx.EndOfIndexEntry = &EndOfIndexEntry{}
		d := &endOfIndexEntryDecoder{r}
		if err := d.Decode(idx.EndOfIndexEntry); err != nil {
			return err
		}

	case bytes.Equal(header[:], linkExtSignature):
		idx.Link = &Link{}
		d := &linkExtensionDecoder{r}
		if err := d.Decode(idx.Link); err != nil {
			return err
		}

	case bytes.Equal(header[:], untrackedCacheExtSignature):
		idx.UntrackedCache = &UntrackedCache{}
		d := &untrackedCacheDecoder{r}
		if err := d.Decode(idx.UntrackedCache); err != nil {
			return err
		}

	case bytes.Equal(header[:], fsMonitorExtSignature):
		idx.FSMonitor = &FSMonitor{}
		d := &fsMonitorDecoder{r}
		if err := d.Decode(idx.FSMonitor); err != nil {
			return err
		}

	case bytes.Equal(header[:], indexEntryOffsetTableExtSignature):
		idx.IndexEntryOffsetTable = &IndexEntryOffsetTable{}
		d := &indexEntryOffsetTableDecoder{r}
		if err := d.Decode(idx.IndexEntryOffsetTable); err != nil {
			return err
		}

	default:
		// See https://git-scm.com/docs/index-format, which says:
		// If the first byte is 'A'..'Z' the extension is optional and can be ignored.
		if header[0] < 'A' || header[0] > 'Z' {
			return ErrUnknownExtension
		}

		d := &unknownExtensionDecoder{r}
		if err := d.Decode(); err != nil {
			return err
		}
	}

	return nil
}

func (d *Decoder) getExtensionReader() (*bufio.Reader, error) {
	len, err := binary.ReadUint32(d.r)
	if err != nil {
		return nil, err
	}

	d.extReader.Reset(&io.LimitedReader{R: d.r, N: int64(len)})
	return d.extReader, nil
}

func (d *Decoder) readChecksum(expected []byte) error {
	var h plumbing.Hash

	if _, err := h.ReadFrom(d.r); err != nil {
		return err
	}

	if h.Compare(expected) != 0 {
		return ErrInvalidChecksum
	}

	return nil
}

func validateHeader(r io.Reader) (version uint32, err error) {
	var s = make([]byte, 4)
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

	return
}

type treeExtensionDecoder struct {
	r *bufio.Reader
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

	countAscii, err := binary.ReadUntil(d.r, ' ')
	if err != nil {
		return nil, err
	}

	count, err := strconv.Atoi(string(countAscii))
	if err != nil {
		return nil, err
	}
	e.Entries = count

	treesAscii, err := binary.ReadUntil(d.r, '\n')
	if err != nil {
		return nil, err
	}

	trees, err := strconv.Atoi(string(treesAscii))
	if err != nil {
		return nil, err
	}
	e.Trees = trees

	// An entry can be in an invalidated state and is represented by having a
	// negative number in the entry_count field.
	if count == -1 {
		return e, nil
	}

	_, err = e.Hash.ReadFrom(d.r)
	if err != nil {
		return nil, err
	}

	return e, nil
}

type resolveUndoDecoder struct {
	r *bufio.Reader
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

	for _, stage := range []Stage{AncestorMode, OurMode, TheirMode} {
		if err := d.readStage(e, Stage(stage)); err != nil {
			return nil, err
		}
	}

	for _, stage := range []Stage{AncestorMode, OurMode, TheirMode} {
		_, ok := e.Stages[stage]
		if !ok {
			continue
		}

		var value plumbing.Hash
		if _, err := value.ReadFrom(d.r); err != nil {
			return nil, err
		}

		e.Stages[stage] = value
	}

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
}

func (d *endOfIndexEntryDecoder) Decode(e *EndOfIndexEntry) error {
	var err error
	e.Offset, err = binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	if _, err := e.Hash.ReadFrom(d.r); err != nil {
		return err
	}

	// Make sure we've consumed the entire extension.
	if d.r.Buffered() > 0 {
		return fmt.Errorf("EIOE extension has extra unparsed data")
	}

	return nil
}

type linkExtensionDecoder struct {
	r *bufio.Reader
}

func (d *linkExtensionDecoder) Decode(ext *Link) error {
	if _, err := ext.ObjectID.ReadFrom(d.r); err != nil {
		return err
	}

	deleteBitmap, err := ewah.ReadFrom(d.r)
	if err != nil {
		return err
	}

	var deleteBuffer bytes.Buffer
	if _, err := deleteBitmap.WriteTo(&deleteBuffer); err != nil {
		return err
	}
	ext.DeleteBitmap = deleteBuffer.Bytes()

	replaceBitmap, err := ewah.ReadFrom(d.r)
	if err != nil {
		return err
	}

	var replaceBuffer bytes.Buffer
	if _, err := replaceBitmap.WriteTo(&replaceBuffer); err != nil {
		return err
	}
	ext.ReplaceBitmap = replaceBuffer.Bytes()

	// Make sure we've consumed the entire extension.
	if d.r.Buffered() > 0 {
		return fmt.Errorf("LINK extension has extra unparsed data")
	}

	return nil
}

type untrackedCacheDecoder struct {
	r *bufio.Reader
}

func (d *untrackedCacheDecoder) Decode(ext *UntrackedCache) error {
	length, err := binary.ReadVariableWidthInt(d.r)
	if err != nil {
		return err
	}
	for i := int64(0); i < length; {
		env, err := binary.ReadUntil(d.r, '\x00')
		if err != nil {
			return err
		}
		ext.Environments = append(ext.Environments, string(env))
		i += int64(len(env)) + 1
	}

	if err := d.decodeUntrackedCacheStats(&ext.InfoExcludeStats); err != nil {
		return err
	}
	if err := d.decodeUntrackedCacheStats(&ext.ExcludesFileStats); err != nil {
		return err
	}

	flags, err := binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	ext.DirFlags = flags

	if _, err := ext.InfoExcludeHash.ReadFrom(d.r); err != nil {
		return err
	}
	if _, err := ext.ExcludesFileHash.ReadFrom(d.r); err != nil {
		return err
	}

	ignoreFile, err := binary.ReadUntil(d.r, '\x00')
	if err != nil {
		return err
	}

	ext.PerDirIgnoreFile = string(ignoreFile)

	count, err := binary.ReadVariableWidthInt(d.r)
	if err != nil {
		return err
	}

	if count != 0 {
		ext.Entries = make([]UntrackedCacheEntry, count)
		for i := int64(0); i < count; i++ {
			entry, err := d.readEntry()
			if err != nil {
				return err
			}
			ext.Entries[i] = *entry
		}

		validBitmap, err := ewah.ReadFrom(d.r)
		if err != nil {
			return err
		}

		validEntries := 0
		for i := uint64(0); i < validBitmap.Bits(); i++ {
			if validBitmap.Get(i) {
				validEntries++
			}
		}

		var validBuffer bytes.Buffer
		if _, err := validBitmap.WriteTo(&validBuffer); err != nil {
			return err
		}
		ext.ValidBitmap = validBuffer.Bytes()

		checkOnlyBitmap, err := ewah.ReadFrom(d.r)
		if err != nil {
			return err
		}

		var checkOnlyBuffer bytes.Buffer
		if _, err := checkOnlyBitmap.WriteTo(&checkOnlyBuffer); err != nil {
			return err
		}
		ext.CheckOnlyBitmap = checkOnlyBuffer.Bytes()

		metadataBitmap, err := ewah.ReadFrom(d.r)
		if err != nil {
			return err
		}

		metadataEntries := 0
		for i := uint64(0); i < metadataBitmap.Bits(); i++ {
			if metadataBitmap.Get(i) {
				metadataEntries++
			}
		}

		var metadataBuffer bytes.Buffer
		if _, err := metadataBitmap.WriteTo(&metadataBuffer); err != nil {
			return err
		}
		ext.MetadataBitmap = metadataBuffer.Bytes()

		ext.Stats = make([]UntrackedCacheStats, validEntries)
		for i := 0; i < validEntries; i++ {
			var value UntrackedCacheStats
			if err := d.decodeUntrackedCacheStats(&value); err != nil {
				return err
			}
			ext.Stats[i] = value
		}

		ext.Hashes = make([]plumbing.Hash, metadataEntries)
		for i := 0; i < metadataEntries; i++ {
			var value plumbing.Hash
			if _, err := value.ReadFrom(d.r); err != nil {
				return err
			}
			ext.Hashes[i] = value
		}

		finalByte, err := d.r.ReadByte()
		if err != nil {
			return err
		}
		if finalByte != 0 {
			return fmt.Errorf("expected final NUL terminator, got: 0x%x", finalByte)
		}
	}

	// Make sure we've consumed the entire extension.
	if d.r.Buffered() > 0 {
		return fmt.Errorf("UNTR extension has extra unparsed data")
	}

	return nil
}

func (d *untrackedCacheDecoder) readEntry() (*UntrackedCacheEntry, error) {
	e := &UntrackedCacheEntry{}

	entries, err := binary.ReadVariableWidthInt(d.r)
	if err != nil {
		return nil, err
	}
	e.Entries = make([]string, entries)

	blocks, err := binary.ReadVariableWidthInt(d.r)
	if err != nil {
		return nil, err
	}
	e.Blocks = blocks

	name, err := binary.ReadUntil(d.r, '\x00')
	if err != nil {
		return nil, err
	}
	e.Name = string(name)

	for i := int64(0); i < entries; i++ {
		value, err := binary.ReadUntil(d.r, '\x00')
		if err != nil {
			return nil, err
		}
		e.Entries[i] = string(value)
	}

	return e, nil
}

func (d *untrackedCacheDecoder) decodeUntrackedCacheStats(e *UntrackedCacheStats) error {
	var msec, mnsec, sec, nsec uint32

	flow := []interface{}{
		&sec, &nsec,
		&msec, &mnsec,
		&e.Dev,
		&e.Inode,
		&e.UID,
		&e.GID,
		&e.Size,
	}

	if err := binary.Read(d.r, flow...); err != nil {
		return err
	}

	if sec != 0 || nsec != 0 {
		e.CreatedAt = time.Unix(int64(sec), int64(nsec))
	}

	if msec != 0 || mnsec != 0 {
		e.ModifiedAt = time.Unix(int64(msec), int64(mnsec))
	}

	return nil
}

type fsMonitorDecoder struct {
	r *bufio.Reader
}

func (d *fsMonitorDecoder) Decode(ext *FSMonitor) error {
	var err error
	ext.Version, err = binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	switch ext.Version {
	case 1:
		var sec, nsec uint32
		if err := binary.Read(d.r, &sec, &nsec); err != nil {
			return err
		}
		if sec != 0 || nsec != 0 {
			ext.Since = time.Unix(int64(sec), int64(nsec))
		}

	case 2:
		token, err := binary.ReadUntil(d.r, '\x00')
		if err != nil {
			return err
		}
		ext.Token = string(token)

	default:
		return errors.New("filesystem monitor cache extension version must be in the range [1, 2]")
	}

	length, err := binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	bitmap := make([]byte, length)
	if err := binary.Read(d.r, bitmap); err != nil {
		return err
	}

	ext.DirtyBitmap = bitmap

	// Make sure we've consumed the entire extension.
	if d.r.Buffered() > 0 {
		return fmt.Errorf("FSMN extension has extra unparsed data")
	}

	return err
}

type indexEntryOffsetTableDecoder struct {
	r *bufio.Reader
}

func (d *indexEntryOffsetTableDecoder) Decode(table *IndexEntryOffsetTable) error {
	var err error

	table.Version, err = binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	for d.r.Buffered() > 0 {
		var entry IndexEntryOffsetEntry

		entry.Offset, err = binary.ReadUint32(d.r)
		if err != nil {
			return err
		}

		entry.Count, err = binary.ReadUint32(d.r)
		if err != nil {
			return err
		}

		table.Entries = append(table.Entries, entry)
	}

	// Make sure we've consumed the entire extension.
	if d.r.Buffered() > 0 {
		return fmt.Errorf("IEOT extension has extra unparsed data")
	}

	return nil
}

type unknownExtensionDecoder struct {
	r *bufio.Reader
}

func (d *unknownExtensionDecoder) Decode() error {
	var buf [1024]byte

	for {
		_, err := d.r.Read(buf[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}
