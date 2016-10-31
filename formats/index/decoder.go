package index

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"strconv"
	"time"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/utils/binary"
)

var (
	// IndexVersionSupported is the range of supported index versions
	IndexVersionSupported = struct{ Min, Max uint32 }{Min: 2, Max: 4}

	// ErrUnsupportedVersion is returned by Decode when the idxindex file
	// version is not supported.
	ErrUnsupportedVersion = errors.New("Unsuported version")
	// ErrMalformedSignature is returned by Decode when the index header file is
	// malformed
	ErrMalformedSignature = errors.New("Malformed index signature file")

	indexSignature          = []byte{'D', 'I', 'R', 'C'}
	treeExtSignature        = []byte{'T', 'R', 'E', 'E'}
	resolveUndoExtSignature = []byte{'R', 'E', 'U', 'C'}
)

const (
	EntryExtended = 0x4000
	EntryValid    = 0x8000

	nameMask         = 0xfff
	intentToAddMask  = 1 << 13
	skipWorkTreeMask = 1 << 14
)

type Decoder struct {
	r         io.Reader
	lastEntry *Entry
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Decode reads the whole index object from its input and stores it in the
// value pointed to by idx.
func (d *Decoder) Decode(idx *Index) error {
	var err error
	idx.Version, err = validateHeader(d.r)
	if err != nil {
		return err
	}

	idx.EntryCount, err = binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	if err := d.readEntries(idx); err != nil {
		return err
	}

	return d.readExtensions(idx)
}

func (d *Decoder) readEntries(idx *Index) error {
	for i := 0; i < int(idx.EntryCount); i++ {
		e, err := d.readEntry(idx)
		if err != nil {
			return err
		}

		d.lastEntry = e
		idx.Entries = append(idx.Entries, *e)
	}

	return nil
}

func (d *Decoder) readEntry(idx *Index) (*Entry, error) {
	e := &Entry{}

	var msec, mnsec, sec, nsec uint32

	flowSize := 62
	flow := []interface{}{
		&msec, &mnsec,
		&sec, &nsec,
		&e.Dev,
		&e.Inode,
		&e.Mode,
		&e.UID,
		&e.GID,
		&e.Size,
		&e.Hash,
		&e.Flags,
	}

	if err := binary.Read(d.r, flow...); err != nil {
		return nil, err
	}

	read := flowSize
	e.CreatedAt = time.Unix(int64(msec), int64(mnsec))
	e.ModifiedAt = time.Unix(int64(sec), int64(nsec))
	e.Stage = Stage(e.Flags>>12) & 0x3

	if e.Flags&EntryExtended != 0 {
		extended, err := binary.ReadUint16(d.r)
		if err != nil {
			return nil, err
		}

		read += 2
		e.IntentToAdd = extended&intentToAddMask != 0
		e.SkipWorktree = extended&skipWorkTreeMask != 0
	}

	if err := d.readEntryName(idx, e); err != nil {
		return nil, err
	}

	return e, d.padEntry(idx, e, read)
}

func (d *Decoder) readEntryName(idx *Index, e *Entry) error {
	var name string
	var err error

	switch idx.Version {
	case 2, 3:
		name, err = d.doReadEntryName(e)
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

func (d *Decoder) doReadEntryName(e *Entry) (string, error) {
	pLen := e.Flags & nameMask

	name := make([]byte, int64(pLen))
	if err := binary.Read(d.r, &name); err != nil {
		return "", err
	}

	return string(name), nil
}

// Index entries are padded out to the next 8 byte alignment
// for historical reasons related to how C Git read the files.
func (d *Decoder) padEntry(idx *Index, e *Entry, read int) error {
	if idx.Version == 4 {
		return nil
	}

	entrySize := read + len(e.Name)
	padLen := 8 - entrySize%8
	if _, err := io.CopyN(ioutil.Discard, d.r, int64(padLen)); err != nil {
		return err
	}

	return nil
}

func (d *Decoder) readExtensions(idx *Index) error {
	var err error
	for {
		err = d.readExtension(idx)
		if err != nil {
			break
		}
	}

	if err == io.EOF {
		return nil
	}

	return err
}

func (d *Decoder) readExtension(idx *Index) error {
	var s = make([]byte, 4)
	if _, err := io.ReadFull(d.r, s); err != nil {
		return err
	}

	len, err := binary.ReadUint32(d.r)
	if err != nil {
		return err
	}

	switch {
	case bytes.Equal(s, treeExtSignature):
		t := &Tree{}
		td := &treeExtensionDecoder{&io.LimitedReader{R: d.r, N: int64(len)}}
		if err := td.Decode(t); err != nil {
			return err
		}

		idx.Cache = t
	case bytes.Equal(s, resolveUndoExtSignature):
		ru := &ResolveUndo{}
		rud := &resolveUndoDecoder{&io.LimitedReader{R: d.r, N: int64(len)}}
		if err := rud.Decode(ru); err != nil {
			return err
		}

		idx.ResolveUndo = ru
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

	if version < IndexVersionSupported.Min || version > IndexVersionSupported.Max {
		return 0, ErrUnsupportedVersion
	}

	return
}

type treeExtensionDecoder struct {
	r io.Reader
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

	// An entry can be in an invalidated state and is represented by having a
	// negative number in the entry_count field.
	if i == -1 {
		return nil, nil
	}

	e.Entries = i
	trees, err := binary.ReadUntil(d.r, '\n')
	if err != nil {
		return nil, err
	}

	i, err = strconv.Atoi(string(trees))
	if err != nil {
		return nil, err
	}

	e.Trees = i

	if err := binary.Read(d.r, &e.Hash); err != nil {
		return nil, err
	}

	return e, nil
}

type resolveUndoDecoder struct {
	r io.Reader
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
		Stages: make(map[Stage]core.Hash, 0),
	}

	path, err := binary.ReadUntil(d.r, '\x00')
	if err != nil {
		return nil, err
	}

	e.Path = string(path)

	for i := 0; i < 3; i++ {
		if err := d.readStage(e, Stage(i+1)); err != nil {
			return nil, err
		}
	}

	for s := range e.Stages {
		var hash core.Hash
		if err := binary.Read(d.r, hash[:]); err != nil {
			return nil, err
		}

		e.Stages[s] = hash
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
		e.Stages[s] = core.ZeroHash
	}

	return nil
}
