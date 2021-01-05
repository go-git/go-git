package dotgit

import (
	"fmt"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/idxfile"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"

	fixtures "github.com/go-git/go-git-fixtures/v4"
	. "gopkg.in/check.v1"
)

func (s *SuiteDotGit) TestNewObjectPack(c *C) {
	f := fixtures.Basic().One()

	fs, clean := s.TemporalFilesystem()
	defer clean()

	dot := New(fs)

	w, err := dot.NewObjectPack()
	c.Assert(err, IsNil)

	_, err = io.Copy(w, f.Packfile())
	c.Assert(err, IsNil)

	c.Assert(w.Close(), IsNil)

	pfPath := fmt.Sprintf("objects/pack/pack-%s.pack", f.PackfileHash)
	idxPath := fmt.Sprintf("objects/pack/pack-%s.idx", f.PackfileHash)

	stat, err := fs.Stat(pfPath)
	c.Assert(err, IsNil)
	c.Assert(stat.Size(), Equals, int64(84794))

	stat, err = fs.Stat(idxPath)
	c.Assert(err, IsNil)
	c.Assert(stat.Size(), Equals, int64(1940))

	pf, err := fs.Open(pfPath)
	c.Assert(err, IsNil)
	pfs := packfile.NewScanner(pf)
	_, objects, err := pfs.Header()
	c.Assert(err, IsNil)
	for i := uint32(0); i < objects; i++ {
		_, err := pfs.NextObjectHeader()
		if err != nil {
			c.Assert(err, IsNil)
			break
		}
	}
	c.Assert(pfs.Close(), IsNil)
}

func (s *SuiteDotGit) TestNewObjectPackUnused(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	dot := New(fs)

	w, err := dot.NewObjectPack()
	c.Assert(err, IsNil)

	c.Assert(w.Close(), IsNil)

	info, err := fs.ReadDir("objects/pack")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 0)

	// check clean up of temporary files
	info, err = fs.ReadDir("")
	c.Assert(err, IsNil)
	for _, fi := range info {
		c.Assert(fi.IsDir(), Equals, true)
	}
}

func (s *SuiteDotGit) TestPackWriterUnusedNotify(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	w, err := newPackWrite(fs)
	c.Assert(err, IsNil)

	w.Notify = func(h plumbing.Hash, idx *idxfile.Writer) {
		c.Fatal("unexpected call to PackWriter.Notify")
	}

	c.Assert(w.Close(), IsNil)
}
