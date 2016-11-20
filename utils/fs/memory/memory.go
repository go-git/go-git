package memory

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/src-d/go-git.v4/utils/fs"
)

const separator = '/'

// Memory a very convenient filesystem based on memory files
type Memory struct {
	base      string
	s         *storage
	tempCount int
}

//New returns a new Memory filesystem
func New() *Memory {
	return &Memory{
		base: "/",
		s:    &storage{make(map[string]*file, 0)},
	}
}

func (fs *Memory) Create(filename string) (fs.File, error) {
	return fs.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0)
}

func (fs *Memory) Open(filename string) (fs.File, error) {
	return fs.OpenFile(filename, os.O_RDONLY, 0)
}

func (fs *Memory) OpenFile(filename string, flag int, perm os.FileMode) (fs.File, error) {
	fullpath := fs.Join(fs.base, filename)
	f, ok := fs.s.files[fullpath]

	if !ok && !isCreate(flag) {
		return nil, os.ErrNotExist
	}

	if f == nil {
		fs.s.files[fullpath] = newFile(fs.base, fullpath, flag)
		return fs.s.files[fullpath], nil
	}

	n := newFile(fs.base, fullpath, flag)
	n.content = f.content

	if isAppend(flag) {
		n.position = int64(n.content.Len())
	}

	if isTruncate(flag) {
		n.content.Truncate()
	}

	return n, nil
}

func (fs *Memory) Stat(filename string) (fs.FileInfo, error) {
	fullpath := fs.Join(fs.base, filename)

	if _, ok := fs.s.files[filename]; ok {
		return newFileInfo(fs.base, fullpath, fs.s.files[filename].content.Len()), nil
	}

	info, err := fs.ReadDir(filename)
	if err == nil && len(info) != 0 {
		return newFileInfo(fs.base, fullpath, len(info)), nil
	}

	return nil, os.ErrNotExist
}

func (fs *Memory) ReadDir(base string) (entries []fs.FileInfo, err error) {
	base = fs.Join(fs.base, base)

	appendedDirs := make(map[string]bool, 0)
	for fullpath, f := range fs.s.files {
		if !strings.HasPrefix(fullpath, base) {
			continue
		}

		fullpath, _ = filepath.Rel(base, fullpath)
		parts := strings.Split(fullpath, string(separator))

		if len(parts) == 1 {
			entries = append(entries, &fileInfo{name: parts[0], size: f.content.Len()})
			continue
		}

		if _, ok := appendedDirs[parts[0]]; ok {
			continue
		}

		entries = append(entries, &fileInfo{name: parts[0], isDir: true})
		appendedDirs[parts[0]] = true
	}

	return
}

var maxTempFiles = 1024 * 4

func (fs *Memory) TempFile(dir, prefix string) (fs.File, error) {
	var fullpath string
	for {
		if fs.tempCount >= maxTempFiles {
			return nil, errors.New("max. number of tempfiles reached")
		}

		fullpath = fs.getTempFilename(dir, prefix)
		if _, ok := fs.s.files[fullpath]; !ok {
			break
		}
	}

	return fs.Create(fullpath)
}

func (fs *Memory) getTempFilename(dir, prefix string) string {
	fs.tempCount++
	filename := fmt.Sprintf("%s_%d_%d", prefix, fs.tempCount, time.Now().UnixNano())
	return fs.Join(fs.base, dir, filename)
}

func (fs *Memory) Rename(from, to string) error {
	from = fs.Join(fs.base, from)
	to = fs.Join(fs.base, to)

	if _, ok := fs.s.files[from]; !ok {
		return os.ErrNotExist
	}

	fs.s.files[to] = fs.s.files[from]
	fs.s.files[to].BaseFilename = to
	delete(fs.s.files, from)

	return nil
}

func (fs *Memory) Remove(filename string) error {
	fullpath := fs.Join(fs.base, filename)
	if _, ok := fs.s.files[fullpath]; !ok {
		return os.ErrNotExist
	}

	delete(fs.s.files, fullpath)
	return nil
}

func (fs *Memory) Join(elem ...string) string {
	return filepath.Join(elem...)
}

func (fs *Memory) Dir(path string) fs.Filesystem {
	return &Memory{
		base: fs.Join(fs.base, path),
		s:    fs.s,
	}
}

func (fs *Memory) Base() string {
	return fs.base
}

type file struct {
	fs.BaseFile

	content  *content
	position int64
	flag     int
}

func newFile(base, fullpath string, flag int) *file {
	filename, _ := filepath.Rel(base, fullpath)

	return &file{
		BaseFile: fs.BaseFile{BaseFilename: filename},
		content:  &content{},
		flag:     flag,
	}
}

func (f *file) Read(b []byte) (int, error) {
	n, err := f.ReadAt(b, f.position)
	if err != nil {
		return 0, err
	}

	return n, err
}

func (f *file) ReadAt(b []byte, off int64) (int, error) {
	if f.IsClosed() {
		return 0, fs.ErrClosed
	}

	if !isReadAndWrite(f.flag) && !isReadOnly(f.flag) {
		return 0, errors.New("read not supported")
	}

	n, err := f.content.ReadAt(b, off)
	f.position += int64(n)

	return n, err
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	if f.IsClosed() {
		return 0, fs.ErrClosed
	}

	switch whence {
	case io.SeekCurrent:
		f.position += offset
	case io.SeekStart:
		f.position = offset
	case io.SeekEnd:
		f.position = int64(f.content.Len()) - offset
	}

	return f.position, nil
}

func (f *file) Write(p []byte) (int, error) {
	if f.IsClosed() {
		return 0, fs.ErrClosed
	}

	if !isReadAndWrite(f.flag) && !isWriteOnly(f.flag) {
		return 0, errors.New("write not supported")
	}

	n, err := f.content.WriteAt(p, f.position)
	f.position += int64(n)

	return n, err
}

func (f *file) Close() error {
	if f.IsClosed() {
		return errors.New("file already closed")
	}

	f.Closed = true
	return nil
}

func (f *file) Open() error {
	f.Closed = false
	return nil
}

type fileInfo struct {
	name  string
	size  int
	isDir bool
}

func newFileInfo(base, fullpath string, size int) *fileInfo {
	filename, _ := filepath.Rel(base, fullpath)

	return &fileInfo{
		name: filename,
		size: size,
	}
}

func (fi *fileInfo) Name() string {
	return fi.name
}

func (fi *fileInfo) Size() int64 {
	return int64(fi.size)
}

func (fi *fileInfo) Mode() os.FileMode {
	return os.FileMode(0)
}

func (*fileInfo) ModTime() time.Time {
	return time.Now()
}

func (fi *fileInfo) IsDir() bool {
	return fi.isDir
}

func (*fileInfo) Sys() interface{} {
	return nil
}

type storage struct {
	files map[string]*file
}

type content struct {
	bytes []byte
}

func (c *content) WriteAt(p []byte, off int64) (int, error) {
	prev := len(c.bytes)
	c.bytes = append(c.bytes[:off], p...)
	if len(c.bytes) < prev {
		c.bytes = c.bytes[:prev]
	}

	return len(p), nil
}

func (c *content) ReadAt(b []byte, off int64) (int, error) {
	size := int64(len(c.bytes))
	if off >= size {
		return 0, io.EOF
	}

	l := int64(len(b))
	if off+l > size {
		l = size - off
	}

	n := copy(b, c.bytes[off:off+l])
	return n, nil
}

func (c *content) Truncate() {
	c.bytes = make([]byte, 0)
}

func (c *content) Len() int {
	return len(c.bytes)
}

func isCreate(flag int) bool {
	return flag&os.O_CREATE != 0
}

func isAppend(flag int) bool {
	return flag&os.O_APPEND != 0
}

func isTruncate(flag int) bool {
	return flag&os.O_TRUNC != 0
}

func isReadAndWrite(flag int) bool {
	return flag&os.O_RDWR != 0
}

func isReadOnly(flag int) bool {
	return flag == os.O_RDONLY
}

func isWriteOnly(flag int) bool {
	return flag&os.O_WRONLY != 0
}
