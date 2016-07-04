package gitdir

import (
	"bufio"
	"errors"
	"io/ioutil"
	"os"
	"strings"

	"gopkg.in/src-d/go-git.v3/core"
)

var (
	// ErrPackedRefsDuplicatedRef is returned when a duplicated
	// reference is found in the packed-ref file. This is usually the
	// case for corrupted git repositories.
	ErrPackedRefsDuplicatedRef = errors.New("duplicated ref found in packed-ref file")
	// ErrPackedRefsBadFormat is returned when the packed-ref file
	// corrupt.
	ErrPackedRefsBadFormat = errors.New("malformed packed-ref")
	// ErrSymRefTargetNotFound is returned when a symbolic reference is
	// targeting a non-existing object. This usually means the
	// repository is corrupt.
	ErrSymRefTargetNotFound = errors.New("symbolic reference target not found")
)

const (
	symRefPrefix = "ref: "
)

func (d *GitDir) addRefsFromPackedRefs() (err error) {
	path := d.fs.Join(d.path, packedRefsPath)
	f, err := d.fs.Open(path)
	if err != nil {
		if err == os.ErrNotExist {
			return nil
		}
		return err
	}
	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if err = d.processLine(line); err != nil {
			return err
		}
	}

	return s.Err()
}

// process lines from a packed-refs file
func (d *GitDir) processLine(line string) error {
	switch line[0] {
	case '#': // comment - ignore
		return nil
	case '^': // annotated tag commit of the previous line - ignore
		return nil
	default:
		ws := strings.Split(line, " ") // hash then ref
		if len(ws) != 2 {
			return ErrPackedRefsBadFormat
		}
		h, r := ws[0], ws[1]

		if _, ok := d.refs[r]; ok {
			return ErrPackedRefsDuplicatedRef
		}
		d.refs[r] = core.NewHash(h)
	}

	return nil
}

func (d *GitDir) addRefsFromRefDir() error {
	return d.walkTree("refs")
}

func (d *GitDir) walkTree(relPath string) error {
	files, err := d.fs.ReadDir(d.fs.Join(d.path, relPath))
	if err != nil {
		return err
	}

	for _, f := range files {
		newRelPath := d.fs.Join(relPath, f.Name())

		if f.IsDir() {
			if err = d.walkTree(newRelPath); err != nil {
				return err
			}
		} else {
			filePath := d.fs.Join(d.path, newRelPath)
			h, err := d.readHashFile(filePath)
			if err != nil {
				return err
			}
			d.refs[newRelPath] = h
		}
	}

	return nil
}

// ReadHashFile reads a single hash from a file.  If a symbolic
// reference is found instead of a hash, the reference is resolved and
// the proper hash is returned.
func (d *GitDir) readHashFile(path string) (h core.Hash, err error) {
	f, err := d.fs.Open(path)
	if err != nil {
		return core.ZeroHash, err
	}
	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return core.ZeroHash, err
	}
	line := strings.TrimSpace(string(b))

	if isSymRef(line) {
		return d.resolveSymRef(line)
	}

	return core.NewHash(line), nil
}

func isSymRef(contents string) bool {
	return strings.HasPrefix(contents, symRefPrefix)
}

func (d *GitDir) resolveSymRef(symRef string) (core.Hash, error) {
	ref := strings.TrimPrefix(symRef, symRefPrefix)

	hash, ok := d.refs[ref]
	if !ok {
		return core.ZeroHash, ErrSymRefTargetNotFound
	}

	return hash, nil
}
