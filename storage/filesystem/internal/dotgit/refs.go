package dotgit

import (
	"bufio"
	"errors"
	"io/ioutil"
	"os"
	"strings"

	"gopkg.in/src-d/go-git.v4/core"
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
	refsPath = "refs"
)

func (d *DotGit) addRefsFromPackedRefs(refs *[]*core.Reference) (err error) {
	f, err := d.fs.Open(packedRefsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	defer func() {
		if errClose := f.Close(); err == nil {
			err = errClose
		}
	}()
	s := bufio.NewScanner(f)
	for s.Scan() {
		ref, err := d.processLine(s.Text())
		if err != nil {
			return err
		}

		if ref != nil {
			*refs = append(*refs, ref)
		}
	}

	return s.Err()
}

// process lines from a packed-refs file
func (d *DotGit) processLine(line string) (*core.Reference, error) {
	switch line[0] {
	case '#': // comment - ignore
		return nil, nil
	case '^': // annotated tag commit of the previous line - ignore
		return nil, nil
	default:
		ws := strings.Split(line, " ") // hash then ref
		if len(ws) != 2 {
			return nil, ErrPackedRefsBadFormat
		}

		return core.NewReferenceFromStrings(ws[1], ws[0]), nil
	}
}

func (d *DotGit) addRefsFromRefDir(refs *[]*core.Reference) error {
	return d.walkReferencesTree(refs, refsPath)
}

func (d *DotGit) walkReferencesTree(refs *[]*core.Reference, relPath string) error {
	files, err := d.fs.ReadDir(relPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	for _, f := range files {
		newRelPath := d.fs.Join(relPath, f.Name())
		if f.IsDir() {
			if err = d.walkReferencesTree(refs, newRelPath); err != nil {
				return err
			}

			continue
		}

		ref, err := d.readReferenceFile(".", newRelPath)
		if err != nil {
			return err
		}

		if ref != nil {
			*refs = append(*refs, ref)
		}
	}

	return nil
}

func (d *DotGit) addRefFromHEAD(refs *[]*core.Reference) error {
	ref, err := d.readReferenceFile(".", "HEAD")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	*refs = append(*refs, ref)
	return nil
}

func (d *DotGit) readReferenceFile(refsPath, refFile string) (ref *core.Reference, err error) {
	path := d.fs.Join(refsPath, refFile)

	f, err := d.fs.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() {
		if errClose := f.Close(); err == nil {
			err = errClose
		}
	}()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	line := strings.TrimSpace(string(b))
	return core.NewReferenceFromStrings(refFile, line), nil
}
