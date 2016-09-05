package fs

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
)

// NewOS returns a new OS.
func NewOS() Filesystem {
	return &OSClient{}
}

// OSClient a filesystem based on OSClient
type OSClient struct {
	RootDir string
}

// NewOSClient returns a new OSClient
func NewOSClient(rootDir string) *OSClient {
	return &OSClient{
		RootDir: rootDir,
	}
}

// Create creates a new GlusterFSFile
func (c *OSClient) Create(filename string) (File, error) {
	fullpath := path.Join(c.RootDir, filename)

	dir := filepath.Dir(fullpath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	f, err := os.Create(fullpath)
	if err != nil {
		return nil, err
	}

	return &OSFile{
		BaseFile: BaseFile{filename: fullpath},
		file:     f,
	}, nil
}

// ReadDir returns the filesystem info for all the archives under the specified
// path.
func (c *OSClient) ReadDir(path string) ([]FileInfo, error) {
	fullpath := c.Join(c.RootDir, path)

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

func (c *OSClient) Rename(from, to string) error {
	if !filepath.IsAbs(from) {
		from = c.Join(c.RootDir, from)
	}

	if !filepath.IsAbs(to) {
		to = c.Join(c.RootDir, to)
	}

	return os.Rename(from, to)
}

func (c *OSClient) Open(filename string) (File, error) {
	fullpath := c.Join(c.RootDir, filename)

	f, err := os.Open(fullpath)
	if err != nil {
		return nil, err
	}

	return &OSFile{
		BaseFile: BaseFile{filename: fullpath},
		file:     f,
	}, nil
}

func (c *OSClient) Stat(filename string) (FileInfo, error) {
	fullpath := c.Join(c.RootDir, filename)
	return os.Stat(fullpath)
}

// Join joins the specified elements using the filesystem separator.
func (c *OSClient) Join(elem ...string) string {
	return filepath.Join(elem...)
}

func (c *OSClient) Dir(path string) Filesystem {
	return NewOSClient(c.Join(c.RootDir, path))
}

func (c *OSClient) Base() string {
	return c.RootDir
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
