// https://github.com/git/git/blob/master/Documentation/gitrepository-layout.txt
package dotgit

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/formats/idxfile"
	"gopkg.in/src-d/go-git.v4/formats/packfile"
	"gopkg.in/src-d/go-git.v4/storage/memory"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

const (
	suffix         = ".git"
	packedRefsPath = "packed-refs"
	configPath     = "config"

	objectsPath = "objects"
	packPath    = "pack"

	packExt = ".pack"
	idxExt  = ".idx"
)

var (
	// ErrNotFound is returned by New when the path is not found.
	ErrNotFound = errors.New("path not found")
	// ErrIdxNotFound is returned by Idxfile when the idx file is not found
	ErrIdxNotFound = errors.New("idx file not found")
	// ErrPackfileNotFound is returned by Packfile when the packfile is not found
	ErrPackfileNotFound = errors.New("packfile not found")
	// ErrConfigNotFound is returned by Config when the config is not found
	ErrConfigNotFound = errors.New("config file not found")
)

// The DotGit type represents a local git repository on disk. This
// type is not zero-value-safe, use the New function to initialize it.
type DotGit struct {
	fs fs.Filesystem
}

// New returns a DotGit value ready to be used. The path argument must
// be the absolute path of a git repository directory (e.g.
// "/foo/bar/.git").
func New(fs fs.Filesystem) *DotGit {
	return &DotGit{fs: fs}
}

func (d *DotGit) ConfigWriter() (fs.File, error) {
	return d.fs.Create(configPath)
}

// Config returns the path of the config file
func (d *DotGit) Config() (fs.File, error) {
	return d.fs.Open(configPath)
}

func (d *DotGit) SetRef(r *core.Reference) error {
	var content string
	switch r.Type() {
	case core.SymbolicReference:
		content = fmt.Sprintf("ref: %s\n", r.Target())
	case core.HashReference:
		content = fmt.Sprintln(r.Hash().String())
	}

	f, err := d.fs.Create(r.Name().String())
	if err != nil {
		return err
	}

	if _, err := f.Write([]byte(content)); err != nil {
		return err
	}
	return f.Close()
}

// Refs scans the git directory collecting references, which it returns.
// Symbolic references are resolved and included in the output.
func (d *DotGit) Refs() ([]*core.Reference, error) {
	var refs []*core.Reference
	if err := d.addRefsFromPackedRefs(&refs); err != nil {
		return nil, err
	}

	if err := d.addRefsFromRefDir(&refs); err != nil {
		return nil, err
	}

	if err := d.addRefFromHEAD(&refs); err != nil {
		return nil, err
	}

	return refs, nil
}

// NewObjectPack return a writer for a new packfile, it saves the packfile to
// disk and also generates and save the index for the given packfile.
func (d *DotGit) NewObjectPack() (*PackWriter, error) {
	return newPackWrite(d.fs)
}

// ObjectPacks returns the list of availables packfiles
func (d *DotGit) ObjectPacks() ([]core.Hash, error) {
	packDir := d.fs.Join(objectsPath, packPath)
	files, err := d.fs.ReadDir(packDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	var packs []core.Hash
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), packExt) {
			continue
		}

		n := f.Name()
		h := core.NewHash(n[5 : len(n)-5]) //pack-(hash).pack
		packs = append(packs, h)

	}

	return packs, nil
}

// ObjectPack returns a fs.File of the given packfile
func (d *DotGit) ObjectPack(hash core.Hash) (fs.File, error) {
	file := d.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s.pack", hash.String()))

	pack, err := d.fs.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPackfileNotFound
		}

		return nil, err
	}

	return pack, nil
}

// ObjectPackIdx returns a fs.File of the index file for a given packfile
func (d *DotGit) ObjectPackIdx(hash core.Hash) (fs.File, error) {
	file := d.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s.idx", hash.String()))
	idx, err := d.fs.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPackfileNotFound
		}

		return nil, err
	}

	return idx, nil
}

// Objects returns a slice with the hashes of objects found under the
// .git/objects/ directory.
func (d *DotGit) Objects() ([]core.Hash, error) {
	files, err := d.fs.ReadDir(objectsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	var objects []core.Hash
	for _, f := range files {
		if f.IsDir() && len(f.Name()) == 2 && isHex(f.Name()) {
			base := f.Name()
			d, err := d.fs.ReadDir(d.fs.Join(objectsPath, base))
			if err != nil {
				return nil, err
			}

			for _, o := range d {
				objects = append(objects, core.NewHash(base+o.Name()))
			}
		}
	}

	return objects, nil
}

// Object return a fs.File poiting the object file, if exists
func (d *DotGit) Object(h core.Hash) (fs.File, error) {
	hash := h.String()
	file := d.fs.Join(objectsPath, hash[0:2], hash[2:40])

	return d.fs.Open(file)
}

func isHex(s string) bool {
	for _, b := range []byte(s) {
		if isNum(b) {
			continue
		}
		if isHexAlpha(b) {
			continue
		}

		return false
	}

	return true
}

func isNum(b byte) bool {
	return b >= '0' && b <= '9'
}

func isHexAlpha(b byte) bool {
	return b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

type PackWriter struct {
	fs fs.Filesystem
	sr io.ReadCloser
	sw io.WriteCloser
	fw fs.File
	mw io.Writer

	checksum core.Hash
	index    idxfile.Idxfile
	result   chan error
	Notify   func(h core.Hash, i idxfile.Idxfile)
}

func newPackWrite(fs fs.Filesystem) (*PackWriter, error) {
	temp := sha1.Sum([]byte(time.Now().String()))
	filename := fmt.Sprintf(".%x", temp)

	fw, err := fs.Create(fs.Join(objectsPath, packPath, filename))
	if err != nil {
		return nil, err
	}

	sr, sw := io.Pipe()

	writer := &PackWriter{
		fs:     fs,
		fw:     fw,
		sr:     sr,
		sw:     sw,
		mw:     io.MultiWriter(sw, fw),
		result: make(chan error),
	}

	go writer.buildIndex()
	return writer, nil
}

func (w *PackWriter) buildIndex() {
	defer w.sr.Close()
	o := memory.NewStorage().ObjectStorage()
	s := packfile.NewScanner(w.sr)
	d := packfile.NewDecoder(s, o)

	checksum, err := d.Decode()
	if err != nil {
		w.result <- err
		return
	}

	w.checksum = checksum
	w.index.PackfileChecksum = checksum
	w.index.Version = idxfile.VersionSupported

	offsets := d.Offsets()
	for h, crc := range d.CRCs() {
		w.index.Add(h, uint64(offsets[h]), crc)
	}

	w.result <- err
}

func (w *PackWriter) Write(p []byte) (int, error) {
	return w.mw.Write(p)
}

func (w *PackWriter) Close() error {
	defer func() {
		close(w.result)
	}()

	if err := w.fw.Close(); err != nil {
		return err
	}

	if err := w.sw.Close(); err != nil {
		return err
	}

	if err := <-w.result; err != nil {
		return err
	}

	if err := w.save(); err != nil {
		return err
	}

	if w.Notify != nil {
		w.Notify(w.checksum, w.index)
	}

	return nil
}

func (w *PackWriter) save() error {
	base := w.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s", w.checksum))
	idx, err := w.fs.Create(fmt.Sprintf("%s.idx", base))
	if err != nil {
		return err
	}

	if err := w.encodeIdx(idx); err != nil {
		return err
	}

	if err := idx.Close(); err != nil {
		return err
	}

	return w.fs.Rename(w.fw.Filename(), fmt.Sprintf("%s.pack", base))
}

func (w *PackWriter) encodeIdx(writer io.Writer) error {
	e := idxfile.NewEncoder(writer)
	_, err := e.Encode(&w.index)
	return err
}
