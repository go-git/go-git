package dotgit

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/go-git/go-git/v5/plumbing/format/objfile"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"

	"github.com/go-git/go-billy/v5"
)

// PackWriter is a io.Writer that generates the packfile index simultaneously,
// a packfile.Decoder is used with a file reader to read the file being written
// this operation is synchronized with the write operations.
// The packfile is written in a temp file, when Close is called this file
// is renamed/moved (depends on the Filesystem implementation) to the final
// location, if the PackWriter is not used, nothing is written
type PackWriter struct {
	Notify func(plumbing.Hash, *idxfile.Writer)

	fs       billy.Filesystem
	frw      billy.File
	checksum plumbing.Hash
	parser   *packfile.Parser
	writer   *idxfile.Writer
}

func newPackWrite(fs billy.Filesystem) (*PackWriter, error) {
	frw, err := fs.TempFile(fs.Join(objectsPath, packPath), "tmp_pack_")
	if err != nil {
		return nil, err
	}

	writer := &PackWriter{
		fs:  fs,
		frw: frw,
	}

	return writer, nil
}

// waitBuildIndex waits until buildIndex function finishes, this can terminate
// with a packfile.ErrEmptyPackfile, this means that nothing was written so we
// ignore the error
func (w *PackWriter) waitBuildIndex() error {
	if _, err := w.frw.Seek(0, io.SeekStart); err != nil {
		return err
	}

	s := packfile.NewScanner(w.frw)
	w.writer = new(idxfile.Writer)
	var err error
	w.parser, err = packfile.NewParser(s, w.writer)
	if err != nil {
		return err
	}

	checksum, err := w.parser.Parse()
	if err != nil {
		if err == packfile.ErrEmptyPackfile {
			return nil
		}
		return err
	}

	w.checksum = checksum
	return err
}

func (w *PackWriter) Write(p []byte) (int, error) {
	return w.frw.Write(p)
}

// Close closes all the file descriptors and save the final packfile, if nothing
// was written, the tempfiles are deleted without writing a packfile.
func (w *PackWriter) Close() error {
	defer func() {
		if w.Notify != nil && w.writer != nil && w.writer.Finished() {
			w.Notify(w.checksum, w.writer)
		}
	}()

	if err := w.waitBuildIndex(); err != nil {
		return err
	}

	if err := w.frw.Close(); err != nil {
		return err
	}

	if w.writer == nil || !w.writer.Finished() {
		return w.clean()
	}

	return w.save()
}

func (w *PackWriter) clean() error {
	return w.fs.Remove(w.frw.Name())
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

	return w.fs.Rename(w.frw.Name(), fmt.Sprintf("%s.pack", base))
}

func (w *PackWriter) encodeIdx(writer io.Writer) error {
	idx, err := w.writer.Index()
	if err != nil {
		return err
	}

	e := idxfile.NewEncoder(writer)
	_, err = e.Encode(idx)
	return err
}

type ObjectWriter struct {
	objfile.Writer
	fs billy.Filesystem
	f  billy.File
}

func newObjectWriter(fs billy.Filesystem) (*ObjectWriter, error) {
	f, err := fs.TempFile(fs.Join(objectsPath, packPath), "tmp_obj_")
	if err != nil {
		return nil, err
	}

	return &ObjectWriter{
		Writer: (*objfile.NewWriter(f)),
		fs:     fs,
		f:      f,
	}, nil
}

func (w *ObjectWriter) Close() error {
	if err := w.Writer.Close(); err != nil {
		return err
	}

	if err := w.f.Close(); err != nil {
		return err
	}

	return w.save()
}

func (w *ObjectWriter) save() error {
	hash := w.Hash().String()
	file := w.fs.Join(objectsPath, hash[0:2], hash[2:40])

	return w.fs.Rename(w.f.Name(), file)
}
