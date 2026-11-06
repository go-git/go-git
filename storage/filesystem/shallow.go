package filesystem

import (
	"bufio"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// ShallowStorage where the shallow commits are stored, an internal to
// manipulate the shallow file
type ShallowStorage struct {
	dir          *dotgit.DotGit
	objectFormat formatcfg.ObjectFormat
}

// SetShallow replaces the shallow file with commits, one hex object ID per
// line.
func (s *ShallowStorage) SetShallow(commits []plumbing.Hash) (err error) {
	f, err := s.dir.ShallowWriter()
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(f, &err)
	w := bufio.NewWriter(f)
	for _, h := range commits {
		if _, err := fmt.Fprintf(w, "%s\n", h); err != nil {
			return err
		}
	}

	return w.Flush()
}

// Shallow returns the shallow commits reading from shallow file from .git.
//
// Each line must start with a full hash in the repository's object format.
// Anything after the hash is ignored, so CRLF line endings are accepted. A
// line that does not start with a full hash is an error, as in upstream's
// is_repository_shallow.
func (s *ShallowStorage) Shallow() ([]plumbing.Hash, error) {
	f, err := s.dir.Shallow()
	if f == nil || err != nil {
		return nil, err
	}

	defer ioutil.CheckClose(f, &err)

	var hash []plumbing.Hash

	hexSize := s.objectFormat.HexSize()
	scn := bufio.NewScanner(f)
	for scn.Scan() {
		line := scn.Text()
		if len(line) < hexSize {
			return nil, fmt.Errorf("bad shallow line: %q", line)
		}
		h, ok := plumbing.FromHex(line[:hexSize])
		if !ok {
			return nil, fmt.Errorf("bad shallow line: %q", line)
		}
		hash = append(hash, h)
	}

	return hash, scn.Err()
}
