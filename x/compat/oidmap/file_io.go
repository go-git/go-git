package oidmap

import (
	"bytes"
	"fmt"
	"os"

	"github.com/go-git/go-billy/v6"

	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

func (m *File) mapDir() string {
	return m.fs.Join(m.path, objectMapDirName)
}

func (m *File) mapPathForData(nativeFmt formatcfg.ObjectFormat, data []byte) (string, error) {
	checksum, err := checksumForFormat(nativeFmt, data)
	if err != nil {
		return "", err
	}
	return m.fs.Join(m.mapDir(), mapFilePrefix+hexFromBytes(checksum)+mapFileExt), nil
}

func (m *File) legacyIdxPath() string {
	return m.fs.Join(m.path, legacyLooseObjectIdxFile)
}

func readFile(fs billy.Filesystem, path string) (data []byte, err error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	var buf bytes.Buffer
	if _, err = buf.ReadFrom(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func atomicWriteFile(fs billy.Filesystem, target string, data []byte, perm os.FileMode) (err error) {
	lockPath := target + ".lock"
	f, err := fs.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("create lock file for %s: %w", target, err)
	}

	closed := false
	committed := false
	defer func() {
		if !closed {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}
		if !committed {
			_ = fs.Remove(lockPath)
		}
	}()

	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("write lock file for %s: %w", target, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close lock file for %s: %w", target, err)
	}
	closed = true
	if chmodFS, ok := fs.(billy.Chmod); ok {
		if err = chmodFS.Chmod(lockPath, perm); err != nil {
			return fmt.Errorf("chmod lock file for %s: %w", target, err)
		}
	}
	if err = fs.Rename(lockPath, target); err != nil {
		return fmt.Errorf("commit lock file for %s: %w", target, err)
	}
	committed = true
	return nil
}
