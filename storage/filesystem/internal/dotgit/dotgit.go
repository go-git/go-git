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
	"gopkg.in/src-d/go-git.v4/storage/filesystem/internal/index"
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
	// ErrObjfileNotFound is returned by Objectfile when the objectffile is not found
	ErrObjfileNotFound = errors.New("object file not found")
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

// Config returns the path of the config file
func (d *DotGit) Config() (fs.File, error) {
	return d.fs.Open(configPath)
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

func (d *DotGit) NewObjectPack() (*PackWriter, error) {
	return newPackWrite(d.fs)
}

// ObjectsPacks returns the list of availables packfiles
func (d *DotGit) ObjectsPacks() ([]fs.FileInfo, error) {
	packDir := d.fs.Join(objectsPath, packPath)
	files, err := d.fs.ReadDir(packDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	var packs []fs.FileInfo
	for _, f := range files {
		if strings.HasSuffix(f.Name(), packExt) {
			packs = append(packs, f)
		}
	}

	return packs, nil
}

// ObjectPack returns the requested packfile and his idx
func (d *DotGit) ObjectPack(filename string) (pack, idx fs.File, err error) {
	if !strings.HasSuffix(filename, packExt) {
		return nil, nil, fmt.Errorf("a .pack file should be provided")
	}

	pack, err = d.fs.Open(d.fs.Join(objectsPath, packPath, filename))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrPackfileNotFound
		}

		return
	}

	idxfile := filename[0:len(filename)-len(packExt)] + idxExt
	idxpath := d.fs.Join(objectsPath, packPath, idxfile)
	idx, err = d.fs.Open(idxpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrIdxNotFound
		}

		return
	}

	return
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
	fs         fs.Filesystem
	file       fs.File
	writer     io.Writer
	pipeReader io.ReadCloser
	pipeWriter io.WriteCloser
	hash       core.Hash
	index      index.Index
	result     chan error
}

func newPackWrite(fs fs.Filesystem) (*PackWriter, error) {
	r, w := io.Pipe()

	temp := sha1.Sum([]byte(time.Now().String()))
	filename := fmt.Sprintf(".%x", temp)

	file, err := fs.Create(fs.Join(objectsPath, packPath, filename))
	if err != nil {
		return nil, err
	}

	writer := &PackWriter{
		fs:         fs,
		file:       file,
		writer:     io.MultiWriter(w, file),
		pipeReader: r,
		pipeWriter: w,
		result:     make(chan error),
	}

	go writer.buildIndex()
	return writer, nil
}

func (w *PackWriter) buildIndex() {
	defer w.pipeReader.Close()
	index, hash, err := index.NewFromPackfileInMemory(w.pipeReader)
	w.index = index
	w.hash = hash

	w.result <- err
}

func (w *PackWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *PackWriter) Close() error {
	defer func() {
		close(w.result)
	}()

	if err := w.file.Close(); err != nil {
		return err
	}

	if err := w.pipeWriter.Close(); err != nil {
		return err
	}

	if err := <-w.result; err != nil {
		return err
	}

	return w.save()
}

func (w *PackWriter) save() error {
	base := w.fs.Join(objectsPath, packPath, fmt.Sprintf("pack-%s", w.hash))

	//idx, err := w.fs.Create(fmt.Sprintf("%s.idx", base))

	return w.fs.Rename(w.file.Filename(), fmt.Sprintf("%s.pack", base))
}
