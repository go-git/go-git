package dotgit

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
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

func (s *SuiteDotGit) TestSyncedReader(c *C) {
	tmpw, err := util.TempFile(osfs.Default, "", "example")
	c.Assert(err, IsNil)

	tmpr, err := osfs.Default.Open(tmpw.Name())
	c.Assert(err, IsNil)

	defer func() {
		tmpw.Close()
		tmpr.Close()
		os.Remove(tmpw.Name())
	}()

	synced := newSyncedReader(tmpw, tmpr)

	go func() {
		for i := 0; i < 281; i++ {
			_, err := synced.Write([]byte(strconv.Itoa(i) + "\n"))
			c.Assert(err, IsNil)
		}

		synced.Close()
	}()

	o, err := synced.Seek(1002, io.SeekStart)
	c.Assert(err, IsNil)
	c.Assert(o, Equals, int64(1002))

	head := make([]byte, 3)
	n, err := io.ReadFull(synced, head)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)
	c.Assert(string(head), Equals, "278")

	o, err = synced.Seek(1010, io.SeekStart)
	c.Assert(err, IsNil)
	c.Assert(o, Equals, int64(1010))

	n, err = io.ReadFull(synced, head)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)
	c.Assert(string(head), Equals, "280")
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
