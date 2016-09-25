package fs

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
)

// OSClient a filesystem based on OSClient
type OS struct {
	RootDir string
}

// NewOSClient returns a new OSClient
func NewOS(rootDir string) *OS {
	return &OS{
		RootDir: rootDir,
	}
}

// Create creates a new GlusterFSFile
func (fs *OS) Create(filename string) (File, error) {
	fullpath := path.Join(fs.RootDir, filename)

	if err := fs.createDir(fullpath); err != nil {
		return nil, err
	}

	f, err := os.Create(fullpath)
	if err != nil {
		return nil, err
	}

	return &OSFile{
		BaseFile: BaseFile{filename: filename},
		file:     f,
	}, nil
}

func (fs *OS) createDir(fullpath string) error {
	dir := filepath.Dir(fullpath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}

// ReadDir returns the filesystem info for all the archives under the specified
// path.
func (fs *OS) ReadDir(path string) ([]FileInfo, error) {
	fullpath := fs.Join(fs.RootDir, path)

	l, err := ioutil.ReadDir(fullpath)
	if err != nil {
		return nil, err
	}

	var s = make([]FileInfo, len(l))
	for i, f := range l {
		s[i] = f
	}

	return s, nil
}

func (fs *OS) Rename(from, to string) error {
	from = fs.Join(fs.RootDir, from)
	to = fs.Join(fs.RootDir, to)

	if err := fs.createDir(to); err != nil {
		return err
	}

	return os.Rename(from, to)
}

func (fs *OS) Open(filename string) (File, error) {
	fullpath := fs.Join(fs.RootDir, filename)

	f, err := os.Open(fullpath)
	if err != nil {
		return nil, err
	}

	return &OSFile{
		BaseFile: BaseFile{filename: filename},
		file:     f,
	}, nil
}

func (fs *OS) Stat(filename string) (FileInfo, error) {
	fullpath := fs.Join(fs.RootDir, filename)
	return os.Stat(fullpath)
}

// Join joins the specified elements using the filesystem separator.
func (fs *OS) Join(elem ...string) string {
	return filepath.Join(elem...)
}

func (fs *OS) Dir(path string) Filesystem {
	return NewOS(fs.Join(fs.RootDir, path))
}

func (fs *OS) Base() string {
	return fs.RootDir
}

type OSFile struct {
	file *os.File
	BaseFile
}

func (f *OSFile) Read(p []byte) (int, error) {
	return f.file.Read(p)
}

func (f *OSFile) Seek(offset int64, whence int) (int64, error) {
	return f.file.Seek(offset, whence)
}

func (f *OSFile) Write(p []byte) (int, error) {
	return f.file.Write(p)
}

func (f *OSFile) Close() error {
	f.closed = true

	return f.file.Close()
}
