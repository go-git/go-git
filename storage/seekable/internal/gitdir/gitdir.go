package gitdir

import (
	"errors"
	"io/ioutil"
	"os"
	"strings"

	"gopkg.in/src-d/go-git.v3/clients/common"
	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/utils/fs"
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

// The GitDir type represents a local git repository on disk. This
// type is not zero-value-safe, use the New function to initialize it.
type GitDir struct {
	fs      fs.FS
	path    string
	refs    map[string]core.Hash
	packDir string
}

// New returns a GitDir value ready to be used. The path argument must
// be the absolute path of a git repository directory (e.g.
// "/foo/bar/.git").
func New(fs fs.FS, path string) (*GitDir, error) {
	d := &GitDir{}
	d.fs = fs
	d.path = path
	d.packDir = d.fs.Join(d.path, "objects", "pack")

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
func (d *GitDir) Refs() (map[string]core.Hash, error) {
	var err error

	d.refs = make(map[string]core.Hash)

	if err = d.addRefsFromPackedRefs(); err != nil {
		return nil, err
	}

	if err = d.addRefsFromRefDir(); err != nil {
		return nil, err
	}

	return d.refs, err
}

// Capabilities scans the git directory collection capabilities, which it returns.
func (d *GitDir) Capabilities() (*common.Capabilities, error) {
	c := common.NewCapabilities()

	err := d.addSymRefCapability(c)

	return c, err
}

func (d *GitDir) addSymRefCapability(cap *common.Capabilities) (err error) {
	f, err := d.fs.Open(d.fs.Join(d.path, "HEAD"))
	if err != nil {
		return err
	}

	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	data := strings.TrimSpace(string(b))

	c := "symref"
	ref := strings.TrimPrefix(data, symRefPrefix)
	cap.Set(c, "HEAD:"+ref)

	return nil
}

// Packfile returns the path of the packfile (really, it returns the
// path of the first file in the "objects/pack/" directory with a
// ".pack" extension.
func (d *GitDir) Packfile() (fs.FS, string, error) {
	files, err := d.fs.ReadDir(d.packDir)
	if err != nil {
		return nil, "", err
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".pack") {
			return d.fs, d.fs.Join(d.packDir, f.Name()), nil
		}
	}

	return nil, "", ErrPackfileNotFound
}

// Idxfile returns the path of the idx file (really, it returns the
// path of the first file in the "objects/pack/" directory with an
// ".idx" extension.
func (d *GitDir) Idxfile() (fs.FS, string, error) {
	files, err := d.fs.ReadDir(d.packDir)
	if err != nil {
		return nil, "", err
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".idx") {
			return d.fs, d.fs.Join(d.packDir, f.Name()), nil
		}
	}

	return nil, "", ErrIdxNotFound
}
