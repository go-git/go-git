package packfile

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/utils/ioutil"
)

var signature = []byte{'P', 'A', 'C', 'K'}

const (
	// VersionSupported is the packfile version supported by this package
	VersionSupported uint32 = 2

	firstLengthBits = uint8(4)   // the first byte into object header has 4 bits to store the length
	lengthBits      = uint8(7)   // subsequent bytes has 7 bits to store the length
	maskFirstLength = 15         // 0000 1111
	maskContinue    = 0x80       // 1000 0000
	maskLength      = uint8(127) // 0111 1111
	maskType        = uint8(112) // 0111 0000
)

// UpdateObjectStorage updates the storer with the objects in the given
// packfile.
func UpdateObjectStorage(s storer.Storer, packfile io.Reader) error {
	if pw, ok := s.(storer.PackfileWriter); ok {
		return WritePackfileToObjectStorage(pw, packfile)
	}

	updater := newPackfileStorageUpdater(s)
	_, err := NewParser(NewScanner(packfile), updater).Parse()
	return err
}

// WritePackfileToObjectStorage writes all the packfile objects into the given
// object storage.
func WritePackfileToObjectStorage(
	sw storer.PackfileWriter,
	packfile io.Reader,
) (err error) {
	w, err := sw.PackfileWriter()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(w, &err)
	_, err = io.Copy(w, packfile)
	return err
}

var bufPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(nil)
	},
}

var errMissingObjectContent = errors.New("missing object content")

type packfileStorageUpdater struct {
	storer.Storer
	lastSize int64
	lastType plumbing.ObjectType
}

func newPackfileStorageUpdater(s storer.Storer) *packfileStorageUpdater {
	return &packfileStorageUpdater{Storer: s}
}

func (p *packfileStorageUpdater) OnHeader(count uint32) error {
	return nil
}

func (p *packfileStorageUpdater) OnInflatedObjectHeader(
	t plumbing.ObjectType,
	objSize int64,
	pos int64,
) error {
	if p.lastSize > 0 || p.lastType != plumbing.InvalidObject {
		return errMissingObjectContent
	}

	p.lastType = t
	p.lastSize = objSize
	return nil
}

func (p *packfileStorageUpdater) OnInflatedObjectContent(
	h plumbing.Hash,
	pos int64,
	crc uint32,
	content []byte,
) error {
	obj := new(plumbing.MemoryObject)
	obj.SetSize(p.lastSize)
	obj.SetType(p.lastType)
	if _, err := obj.Write(content); err != nil {
		return err
	}

	_, err := p.SetEncodedObject(obj)
	p.lastSize = 0
	p.lastType = plumbing.InvalidObject
	return err
}

func (p *packfileStorageUpdater) OnFooter(h plumbing.Hash) error {
	return nil
}
