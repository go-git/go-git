package dotgit

import (
	"errors"
	"os"
	"strings"

	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

const (
	suffix         = ".git"
	packedRefsPath = "packed-refs"
)

var (
	// ErrNotFound is returned by New when the path is not found.
	ErrNotFound = errors.New("path not found")
	// ErrIdxNotFound is returned by Idxfile when the idx file is not found on the
	// repository.
	ErrIdxNotFound = errors.New("idx file not found")
	// ErrPackfileNotFound is returned by Packfile when the packfile is not found
	// on the repository.
	ErrPackfileNotFound = errors.New("packfile not found")
)

// The DotGit type represents a local git repository on disk. This
// type is not zero-value-safe, use the New function to initialize it.
type DotGit struct {
	fs   fs.FS
	path string
}

// New returns a DotGit value ready to be used. The path argument must
// be the absolute path of a git repository directory (e.g.
// "/foo/bar/.git").
func New(fs fs.FS, path string) (*DotGit, error) {
	d := &DotGit{fs: fs, path: path}
	if _, err := fs.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return d, nil
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

// Packfile returns the path of the packfile (really, it returns the
// path of the first file in the "objects/pack/" directory with a
// ".pack" extension.
func (d *DotGit) Packfile() (fs.FS, string, error) {
	packDir := d.fs.Join(d.path, "objects", "pack")
	files, err := d.fs.ReadDir(packDir)
	if err != nil {
		return nil, "", err
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".pack") {
			return d.fs, d.fs.Join(packDir, f.Name()), nil
		}
	}

	return nil, "", ErrPackfileNotFound
}

// Idxfile returns the path of the idx file (really, it returns the
// path of the first file in the "objects/pack/" directory with an
// ".idx" extension.
func (d *DotGit) Idxfile() (fs.FS, string, error) {
	packDir := d.fs.Join(d.path, "objects", "pack")
	files, err := d.fs.ReadDir(packDir)
	if err != nil {
		return nil, "", err
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".idx") {
			return d.fs, d.fs.Join(packDir, f.Name()), nil
		}
	}

	return nil, "", ErrIdxNotFound
}
