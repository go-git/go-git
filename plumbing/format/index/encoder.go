package index

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

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
	hash      hash.Hash
	lastEntry *Entry
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	// TODO: Support passing an ObjectFormat (sha256)
	h := hash.New(crypto.SHA1)
	mw := io.MultiWriter(w, h)
	return &Encoder{mw, h, nil}
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
	sort.Sort(byName(idx.Entries))

	for _, entry := range idx.Entries {
		if err := e.encodeEntry(idx, entry); err != nil {
			return err
		}
		entryLength := entryHeaderLength
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

	flow := []interface{}{
		sec, nsec,
		msec, mnsec,
		entry.Dev,
		entry.Inode,
		entry.Mode,
		entry.UID,
		entry.GID,
		entry.Size,
		entry.Hash.Bytes(),
	}

	flagsFlow := []interface{}{flags}

	if entry.IntentToAdd || entry.SkipWorktree {
		var extendedFlags uint16

		if entry.IntentToAdd {
			extendedFlags |= intentToAddMask
		}
		if entry.SkipWorktree {
			extendedFlags |= skipWorkTreeMask
		}

		flagsFlow = []interface{}{flags | entryExtended, extendedFlags}
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
	name := entry.Name
	l := 0
	if e.lastEntry != nil {
		dir := path.Dir(e.lastEntry.Name) + "/"
		if strings.HasPrefix(entry.Name, dir) {
			l = len(e.lastEntry.Name) - len(dir)
			name = strings.TrimPrefix(entry.Name, dir)
		} else {
			l = len(e.lastEntry.Name)
		}
	}

	e.lastEntry = entry

	err := binary.WriteVariableWidthInt(e.w, int64(l))
	if err != nil {
		return err
	}

	return binary.Write(e.w, []byte(name+string('\x00')))
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

	return nil
}

func (e *Encoder) encodeExtensions(idx *Index) error {
	// Note: always write EOIE first to mark the boundary.
	if idx.EndOfIndexEntry != nil {
		if err := e.encodeEOIE(idx.EndOfIndexEntry); err != nil {
			return err
		}
	}

	// Write all the other optional extensions after the EIOE.
	if idx.Cache != nil {
		if err := e.encodeTREE(idx.Cache); err != nil {
			return err
		}
	}

	if idx.Link != nil {
		if err := e.encodeLINK(idx.Link); err != nil {
			return err
		}
	}

	if idx.UntrackedCache != nil {
		if err := e.encodeUNTR(idx.UntrackedCache); err != nil {
			return err
		}
	}

	if idx.ResolveUndo != nil {
		if err := e.encodeREUC(idx.ResolveUndo); err != nil {
			return err
		}
	}

	return nil
}

func (e *Encoder) encodeEOIE(ext *EndOfIndexEntry) error {
	buf := &bytes.Buffer{}
	if err := binary.WriteUint32(buf, ext.Offset); err != nil {
		return err
	}
	if _, err := ext.Hash.WriteTo(buf); err != nil {
		return err
	}
	return e.encodeRawExtension("EOIE", buf.Bytes())
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
		if _, err := buf.Write(i.Hash.Bytes()); err != nil {
			return err
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

		for _, stage := range []Stage{AncestorMode, OurMode, TheirMode} {
			if _, ok := i.Stages[stage]; ok {
				if _, err := buf.WriteString(strconv.FormatInt(int64(stage), 10)); err != nil {
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
		for _, stage := range []Stage{AncestorMode, OurMode, TheirMode} {
			hash, ok := i.Stages[stage]
			if !ok {
				continue
			}
			if _, err := buf.Write(hash.Bytes()); err != nil {
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
	if err := binary.WriteUint32(buf, uint32(len(ext.Delete))); err != nil {
		return err
	}
	if _, err := buf.Write(ext.Delete); err != nil {
		return err
	}
	if err := binary.WriteUint32(buf, uint32(len(ext.Replace))); err != nil {
		return err
	}
	if _, err := buf.Write(ext.Replace); err != nil {
		return err
	}
	return e.encodeRawExtension("link", buf.Bytes())
}

func (e *Encoder) encodeUNTR(ext *UntrackedCache) error {
	buf := &bytes.Buffer{}
	for _, i := range ext.Environments {
		if _, err := buf.WriteString(i); err != nil {
			return err
		}
		if err := buf.WriteByte(0); err != nil {
			return err
		}
	}
	// Terminate the list of strings with a NUL value.
	if err := buf.WriteByte(0); err != nil {
		return err
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
	}

	// Terminate the whole extension with a final NUL value.
	if err := buf.WriteByte(0); err != nil {
		return err
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

	flow := []interface{}{
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
	return binary.Write(e.w, e.hash.Sum(nil))
}

type byName []*Entry

func (l byName) Len() int           { return len(l) }
func (l byName) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }
func (l byName) Less(i, j int) bool { return l[i].Name < l[j].Name }
