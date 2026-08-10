package index

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	format "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/hash"
	"github.com/go-git/go-git/v6/utils/binary"
)

var (
	// EncodeVersionSupported is the range of supported index versions
	EncodeVersionSupported uint32 = 4

	// ErrInvalidTimestamp is returned by Encode if a Index with a Entry with
	// negative timestamp values
	ErrInvalidTimestamp = errors.New("negative timestamps are not allowed")
)

// An Encoder writes an Index to an output stream.
type Encoder struct {
	w         io.Writer
	counter   *countingWriter
	hash      hash.Hash
	lastEntry *Entry
	skipHash  bool

	// entriesStart is the absolute byte offset at which the index entries
	// begin (immediately after the 12-byte header).
	entriesStart int64
	// entryOffsets holds the absolute byte offset of each entry in write
	// order. It is used to recompute IEOT block offsets at write time.
	entryOffsets []int64
	// extHeaders accumulates the 8-byte (signature, size) header of each
	// extension written before EOIE, which is the input to the EOIE hash. It
	// is nil unless an EOIE extension is going to be written.
	extHeaders *bytes.Buffer
}

// countingWriter wraps a writer and tracks the number of bytes written so the
// encoder can record absolute offsets for the EOIE and IEOT extensions.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer, h hash.Hash, opts ...Option) *Encoder {
	var cfg options
	for _, o := range opts {
		o(&cfg)
	}

	e := &Encoder{
		hash:     h,
		skipHash: cfg.skipHash,
	}

	var dst io.Writer
	if e.skipHash {
		dst = w
	} else {
		h.Reset()
		dst = io.MultiWriter(w, h)
	}

	e.counter = &countingWriter{w: dst}
	e.w = e.counter

	return e
}

// Encode writes the Index to the stream of the encoder.
func (e *Encoder) Encode(idx *Index) error {
	return e.encode(idx, true)
}

func (e *Encoder) encode(idx *Index, footer bool) error {
	if idx.Version > EncodeVersionSupported {
		return ErrUnsupportedVersion
	}

	if err := e.encodeHeader(idx); err != nil {
		return err
	}

	if err := e.encodeEntries(idx); err != nil {
		return err
	}

	if err := e.encodeExtensions(idx); err != nil {
		return err
	}

	if footer {
		return e.encodeFooter()
	}
	return nil
}

func (e *Encoder) encodeHeader(idx *Index) error {
	return binary.Write(e.w,
		indexSignature,
		idx.Version,
		uint32(len(idx.Entries)),
	)
}

func (e *Encoder) encodeEntries(idx *Index) error {
	// Stable sort so entries that compare equal keep their input order.
	// Split-index replacement entries all carry a zero-length name (and the
	// same stage) and must stay in base-position order to align with the
	// replace bitmap; an unstable sort would scramble them. byNameAndStage also
	// orders same-name conflict entries by stage.
	sort.Stable(byNameAndStage(idx.Entries))

	// Record where the entries begin and the offset of each entry so IEOT
	// block offsets can be recomputed from the actual byte layout.
	e.entriesStart = e.counter.n
	e.entryOffsets = make([]int64, 0, len(idx.Entries))

	for _, entry := range idx.Entries {
		e.entryOffsets = append(e.entryOffsets, e.counter.n)
		if err := e.encodeEntry(idx, entry); err != nil {
			return err
		}
		entryLength := entryHeaderLength + e.hash.Size()
		if entry.IntentToAdd || entry.SkipWorktree {
			entryLength += 2
		}

		wrote := entryLength + len(entry.Name)
		if err := e.padEntry(idx, wrote); err != nil {
			return err
		}
	}

	return nil
}

func (e *Encoder) encodeEntry(idx *Index, entry *Entry) error {
	sec, nsec, err := e.timeToUint32(&entry.CreatedAt)
	if err != nil {
		return err
	}

	msec, mnsec, err := e.timeToUint32(&entry.ModifiedAt)
	if err != nil {
		return err
	}

	flags := uint16(entry.Stage&0x3) << 12
	if l := len(entry.Name); l < nameMask {
		flags |= uint16(l)
	} else {
		flags |= nameMask
	}

	flagsFlow := []any{flags}

	flow := make([]any, 0, 11+len(flagsFlow))
	flow = append(flow,
		sec, nsec,
		msec, mnsec,
		entry.Dev,
		entry.Inode,
		entry.Mode,
		entry.UID,
		entry.GID,
		entry.Size,
		entry.Hash.Bytes(),
	)

	if entry.IntentToAdd || entry.SkipWorktree {
		var extendedFlags uint16

		if entry.IntentToAdd {
			extendedFlags |= intentToAddMask
		}
		if entry.SkipWorktree {
			extendedFlags |= skipWorkTreeMask
		}

		flagsFlow = []any{flags | entryExtended, extendedFlags}
	}

	flow = append(flow, flagsFlow...)

	if err := binary.Write(e.w, flow...); err != nil {
		return err
	}

	switch idx.Version {
	case 2, 3:
		err = e.encodeEntryName(entry)
	case 4:
		err = e.encodeEntryNameV4(entry)
	default:
		err = ErrUnsupportedVersion
	}

	return err
}

func (e *Encoder) encodeEntryName(entry *Entry) error {
	return binary.Write(e.w, []byte(entry.Name))
}

func (e *Encoder) encodeEntryNameV4(entry *Entry) error {
	// V4 prefix compression: find the longest common prefix between the
	// previous entry's name and the current one. The strip length tells
	// the decoder how many bytes to remove from the end of the previous
	// name, and the suffix is the remainder of the current name.
	prefix := 0
	if e.lastEntry != nil {
		prefix = commonPrefixLen(e.lastEntry.Name, entry.Name)
	}
	stripLen := 0
	if e.lastEntry != nil {
		stripLen = len(e.lastEntry.Name) - prefix
	}

	e.lastEntry = entry

	if err := binary.WriteVariableWidthInt(e.w, int64(stripLen)); err != nil {
		return err
	}

	suffix := entry.Name[prefix:]
	return binary.Write(e.w, append([]byte(suffix), '\x00'))
}

// commonPrefixLen returns the length of the longest common byte prefix
// between a and b.
func commonPrefixLen(a, b string) int {
	n := min(len(b), len(a))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func (e *Encoder) encodeRawExtension(signature string, data []byte) error {
	if len(signature) != 4 {
		return fmt.Errorf("invalid signature length")
	}

	_, err := e.w.Write([]byte(signature))
	if err != nil {
		return err
	}

	err = binary.WriteUint32(e.w, uint32(len(data)))
	if err != nil {
		return err
	}

	_, err = e.w.Write(data)
	if err != nil {
		return err
	}

	// The EOIE hash covers the (signature, size) header of every extension
	// written before it. Record this one while tracking is active.
	if e.extHeaders != nil {
		e.extHeaders.WriteString(signature)
		if err := binary.WriteUint32(e.extHeaders, uint32(len(data))); err != nil {
			return err
		}
	}

	return nil
}

func (e *Encoder) encodeExtensions(idx *Index) error {
	// EOIE points at the start of the extension section and hashes the headers
	// of the extensions preceding it, so both are derived here from the bytes
	// actually written rather than replayed from a previous decode. Start
	// recording extension headers before writing the first one.
	extStart := e.counter.n
	if idx.EndOfIndexEntry != nil {
		e.extHeaders = &bytes.Buffer{}
	}

	// git's do_write_index writes extensions in this order: IEOT, LINK, TREE,
	// REUC, UNTR, FSMONITOR, then EOIE last. Parsing is order-independent, but
	// matching git keeps a git-written index byte-identical across a
	// decode/encode round trip and feeds the EOIE hash the same header sequence.
	if idx.IndexEntryOffsetTable != nil {
		if err := e.encodeIEOT(idx.IndexEntryOffsetTable); err != nil {
			return err
		}
	}

	if idx.Link != nil {
		if err := e.encodeLINK(idx.Link); err != nil {
			return err
		}
	}

	if idx.Cache != nil {
		if err := e.encodeTREE(idx.Cache); err != nil {
			return err
		}
	}

	if idx.ResolveUndo != nil {
		if err := e.encodeREUC(idx.ResolveUndo); err != nil {
			return err
		}
	}

	if idx.UntrackedCache != nil {
		if err := e.encodeUNTR(idx.UntrackedCache); err != nil {
			return err
		}
	}

	if idx.FSMonitor != nil {
		if err := e.encodeFSMN(idx.FSMonitor); err != nil {
			return err
		}
	}

	// EOIE is always last, so its own header is not part of its hash.
	if idx.EndOfIndexEntry != nil {
		if err := e.encodeEOIE(extStart); err != nil {
			return err
		}
	}

	return nil
}

func (e *Encoder) encodeEOIE(extStart int64) error {
	// The offset is the start of the extension section; the hash covers the
	// (signature, size) headers of every extension written before EOIE.
	digest := e.extensionDigest(e.extHeaders.Bytes())
	e.extHeaders = nil

	var h plumbing.Hash
	h.ResetBySize(e.hash.Size())
	if _, err := h.Write(digest); err != nil {
		return err
	}

	buf := &bytes.Buffer{}
	if err := binary.WriteUint32(buf, uint32(extStart)); err != nil {
		return err
	}
	if _, err := h.WriteTo(buf); err != nil {
		return err
	}
	return e.encodeRawExtension("EOIE", buf.Bytes())
}

// extensionDigest hashes data with the index's object-format hash, used to
// compute the EOIE extension hash over the preceding extension headers.
func (e *Encoder) extensionDigest(data []byte) []byte {
	var h hash.Hash
	if e.hash.Size() == format.SHA256Size {
		h = hash.New(crypto.SHA256)
	} else {
		h = hash.New(crypto.SHA1)
	}
	h.Write(data)
	return h.Sum(nil)
}

func (e *Encoder) encodeTREE(ext *Tree) error {
	buf := &bytes.Buffer{}
	for _, i := range ext.Entries {
		if _, err := buf.WriteString(i.Path); err != nil {
			return err
		}
		if err := buf.WriteByte(0); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(buf, "%d %d\n", i.Entries, i.Trees); err != nil {
			return err
		}
		// An invalidated entry (negative entry count) carries no object name; the
		// SHA is omitted, matching git and the decoder which skips it for i.Entries < 0.
		if i.Entries >= 0 {
			if _, err := buf.Write(i.Hash.Bytes()); err != nil {
				return err
			}
		}
	}

	return e.encodeRawExtension("TREE", buf.Bytes())
}

func (e *Encoder) encodeREUC(ext *ResolveUndo) error {
	buf := &bytes.Buffer{}
	for _, i := range ext.Entries {
		if _, err := buf.WriteString(i.Path); err != nil {
			return err
		}
		if err := buf.WriteByte(0); err != nil {
			return err
		}

		// git writes the octal file mode for each of the three stages, using a
		// zero mode for stages absent from the conflict.
		for _, stage := range []Stage{AncestorMode, OurMode, TheirMode} {
			if st, ok := i.Stages[stage]; ok {
				if _, err := buf.WriteString(strconv.FormatInt(int64(st.Mode), 8)); err != nil {
					return err
				}
			} else {
				if _, err := buf.WriteString("0"); err != nil {
					return err
				}
			}
			if err := buf.WriteByte(0); err != nil {
				return err
			}
		}
		// The object names follow, in the same stage order and only for the
		// stages present in the conflict.
		for _, stage := range []Stage{AncestorMode, OurMode, TheirMode} {
			st, ok := i.Stages[stage]
			if !ok {
				continue
			}
			if _, err := buf.Write(st.Hash.Bytes()); err != nil {
				return err
			}
		}
	}
	return e.encodeRawExtension("REUC", buf.Bytes())
}

func (e *Encoder) encodeLINK(ext *Link) error {
	buf := &bytes.Buffer{}
	if _, err := buf.Write(ext.ObjectID.Bytes()); err != nil {
		return err
	}
	if _, err := buf.Write(ext.DeleteBitmap); err != nil {
		return err
	}
	if _, err := buf.Write(ext.ReplaceBitmap); err != nil {
		return err
	}
	return e.encodeRawExtension("link", buf.Bytes())
}

func (e *Encoder) encodeUNTR(ext *UntrackedCache) error {
	buf := &bytes.Buffer{}
	envs := 0
	for _, i := range ext.Environments {
		envs += len(i) + 1
	}
	if err := binary.WriteVariableWidthInt(buf, int64(envs)); err != nil {
		return err
	}
	for _, i := range ext.Environments {
		if _, err := buf.WriteString(i); err != nil {
			return err
		}
		if err := buf.WriteByte(0); err != nil {
			return err
		}
	}
	if err := e.encodeUntrackedCacheStats(buf, &ext.InfoExcludeStats); err != nil {
		return err
	}
	if err := e.encodeUntrackedCacheStats(buf, &ext.ExcludesFileStats); err != nil {
		return err
	}
	if err := binary.WriteUint32(buf, ext.DirFlags); err != nil {
		return err
	}
	if _, err := buf.Write(ext.InfoExcludeHash.Bytes()); err != nil {
		return err
	}
	if _, err := buf.Write(ext.ExcludesFileHash.Bytes()); err != nil {
		return err
	}
	if _, err := buf.WriteString(ext.PerDirIgnoreFile); err != nil {
		return err
	}
	if err := buf.WriteByte(0); err != nil {
		return err
	}
	if err := binary.WriteVariableWidthInt(buf, int64(len(ext.Entries))); err != nil {
		return err
	}
	if len(ext.Entries) != 0 {
		for _, i := range ext.Entries {
			if err := e.encodeUntrackedCacheEntry(buf, &i); err != nil {
				return err
			}
		}
		if _, err := buf.Write(ext.ValidBitmap); err != nil {
			return err
		}
		if _, err := buf.Write(ext.CheckOnlyBitmap); err != nil {
			return err
		}
		if _, err := buf.Write(ext.MetadataBitmap); err != nil {
			return err
		}
		for _, i := range ext.Stats {
			if err := e.encodeUntrackedCacheStats(buf, &i); err != nil {
				return err
			}
		}
		for _, i := range ext.Hashes {
			if _, err := buf.Write(i.Bytes()); err != nil {
				return err
			}
		}
		// Terminate the list with a final NUL value.
		if err := buf.WriteByte(0); err != nil {
			return err
		}
	}

	return e.encodeRawExtension("UNTR", buf.Bytes())
}

func (e *Encoder) encodeUntrackedCacheEntry(w io.Writer, entry *UntrackedCacheEntry) error {
	if err := binary.WriteVariableWidthInt(w, int64(len(entry.Entries))); err != nil {
		return err
	}
	if err := binary.WriteVariableWidthInt(w, entry.Blocks); err != nil {
		return err
	}
	if _, err := w.Write([]byte(entry.Name)); err != nil {
		return err
	}
	if err := binary.Write(w, []byte{'\x00'}); err != nil {
		return err
	}
	for _, i := range entry.Entries {
		if _, err := w.Write([]byte(i)); err != nil {
			return err
		}
		if err := binary.Write(w, []byte{'\x00'}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Encoder) encodeUntrackedCacheStats(w io.Writer, stat *UntrackedCacheStats) error {
	sec, nsec, err := e.timeToUint32(&stat.CreatedAt)
	if err != nil {
		return err
	}

	msec, mnsec, err := e.timeToUint32(&stat.ModifiedAt)
	if err != nil {
		return err
	}

	flow := []any{
		sec, nsec,
		msec, mnsec,
		stat.Dev,
		stat.Inode,
		stat.UID,
		stat.GID,
		stat.Size,
	}

	if err := binary.Write(w, flow...); err != nil {
		return err
	}

	return nil
}

func (e *Encoder) encodeFSMN(ext *FSMonitor) error {
	// Only version 2 is supported; git no longer writes version 1.
	if ext.Version != 2 {
		return fmt.Errorf("unsupported filesystem monitor cache extension version %d, only version 2 is supported", ext.Version)
	}

	buf := &bytes.Buffer{}
	if err := binary.WriteUint32(buf, ext.Version); err != nil {
		return err
	}
	if _, err := buf.Write([]byte(ext.Token)); err != nil {
		return err
	}
	if err := buf.WriteByte(0); err != nil {
		return err
	}
	if err := binary.WriteUint32(buf, uint32(len(ext.DirtyBitmap))); err != nil {
		return err
	}
	if _, err := buf.Write(ext.DirtyBitmap); err != nil {
		return err
	}
	return e.encodeRawExtension("FSMN", buf.Bytes())
}

func (e *Encoder) encodeIEOT(ext *EntryOffsetTable) error {
	buf := &bytes.Buffer{}

	if err := binary.Write(buf, ext.Version); err != nil {
		return err
	}

	// git seeks straight to each block offset with no further validation, so
	// the offsets must point at real entry boundaries in the file being
	// written. Recompute them from the recorded entry positions rather than
	// replaying the decoded (now stale) offsets.
	entry := 0
	for _, count := range e.ieotBlockCounts(ext) {
		offset := e.entriesStart
		if entry < len(e.entryOffsets) {
			offset = e.entryOffsets[entry]
		}
		if err := binary.Write(buf, uint32(offset)); err != nil {
			return err
		}
		if err := binary.Write(buf, count); err != nil {
			return err
		}
		entry += int(count)
	}

	return e.encodeRawExtension("IEOT", buf.Bytes())
}

// ieotBlockCounts returns the entry count of each IEOT block. It preserves the
// decoded partition when its counts still sum to the number of entries being
// written; otherwise the partition is stale (entries were added or removed) and
// a single block covering every entry is used so the offsets stay valid.
func (e *Encoder) ieotBlockCounts(ext *EntryOffsetTable) []uint32 {
	total := len(e.entryOffsets)

	sum := 0
	for _, block := range ext.Entries {
		sum += int(block.Count)
	}

	if len(ext.Entries) > 0 && sum == total {
		counts := make([]uint32, len(ext.Entries))
		for i, block := range ext.Entries {
			counts[i] = block.Count
		}
		return counts
	}

	return []uint32{uint32(total)}
}

func (e *Encoder) timeToUint32(t *time.Time) (uint32, uint32, error) {
	if t.IsZero() {
		return 0, 0, nil
	}

	if t.Unix() < 0 || t.UnixNano() < 0 {
		return 0, 0, ErrInvalidTimestamp
	}

	return uint32(t.Unix()), uint32(t.Nanosecond()), nil
}

func (e *Encoder) padEntry(idx *Index, wrote int) error {
	if idx.Version == 4 {
		return nil
	}

	padLen := 8 - wrote%8

	_, err := e.w.Write(bytes.Repeat([]byte{'\x00'}, padLen))
	return err
}

func (e *Encoder) encodeFooter() error {
	if e.skipHash {
		_, err := e.w.Write(make([]byte, e.hash.Size()))
		return err
	}
	return binary.Write(e.w, e.hash.Sum(nil))
}

type byNameAndStage []*Entry

func (l byNameAndStage) Len() int      { return len(l) }
func (l byNameAndStage) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
func (l byNameAndStage) Less(i, j int) bool {
	if l[i].Name == l[j].Name {
		return l[i].Stage < l[j].Stage
	}
	return l[i].Name < l[j].Name
}
