package dotgit

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strconv"

	"gopkg.in/src-d/go-git.v4/fixtures"
	osfs "gopkg.in/src-d/go-git.v4/utils/fs/os"

	. "gopkg.in/check.v1"
)

func (s *SuiteDotGit) TestNewObjectPack(c *C) {
	f := fixtures.Basic().One()

	dir, err := ioutil.TempDir("", "example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(dir)

	fs := osfs.New(dir)
	dot := New(fs)

	w, err := dot.NewObjectPack()
	c.Assert(err, IsNil)

	_, err = io.Copy(w, f.Packfile())
	c.Assert(err, IsNil)

	c.Assert(w.Close(), IsNil)

	stat, err := fs.Stat(fmt.Sprintf("objects/pack/pack-%s.pack", f.PackfileHash))
	c.Assert(err, IsNil)
	c.Assert(stat.Size(), Equals, int64(84794))

	stat, err = fs.Stat(fmt.Sprintf("objects/pack/pack-%s.idx", f.PackfileHash))
	c.Assert(err, IsNil)
	c.Assert(stat.Size(), Equals, int64(1940))
}

func (s *SuiteDotGit) TestNewObjectPackUnused(c *C) {
	dir, err := ioutil.TempDir("", "example")
	if err != nil {
		log.Fatal(err)
	}

	defer os.RemoveAll(dir)

	fs := osfs.New(dir)
	dot := New(fs)

	w, err := dot.NewObjectPack()
	c.Assert(err, IsNil)

	c.Assert(w.Close(), IsNil)

	info, err := fs.ReadDir("objects/pack")
	c.Assert(err, IsNil)
	c.Assert(info, HasLen, 0)
}

func (s *SuiteDotGit) TestSyncedReader(c *C) {
	tmpw, err := ioutil.TempFile("", "example")
	c.Assert(err, IsNil)

	tmpr, err := os.Open(tmpw.Name())
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
