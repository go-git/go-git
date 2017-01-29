package git

import (
	"fmt"
	"io/ioutil"
	"os"

	"gopkg.in/src-d/go-git.v4/plumbing/format/index"

	. "gopkg.in/check.v1"
	"srcd.works/go-billy.v1/memory"
	osfs "srcd.works/go-billy.v1/os"
)

type WorktreeSuite struct {
	BaseSuite
}

var _ = Suite(&WorktreeSuite{})

func (s *WorktreeSuite) SetUpTest(c *C) {
	s.buildBasicRepository(c)
	// the index is removed if not the Repository will be not clean
	c.Assert(s.Repository.s.SetIndex(&index.Index{Version: 2}), IsNil)
}

func (s *WorktreeSuite) TestCheckout(c *C) {
	h, err := s.Repository.Head()
	c.Assert(err, IsNil)

	fs := memory.New()
	w := &Worktree{
		r:  s.Repository,
		fs: fs,
	}

	err = w.Checkout(h.Hash())
	c.Assert(err, IsNil)

	entries, err := fs.ReadDir("/")
	c.Assert(err, IsNil)

	c.Assert(entries, HasLen, 8)
	ch, err := fs.Open("CHANGELOG")
	c.Assert(err, IsNil)

	content, err := ioutil.ReadAll(ch)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "Initial changelog\n")

	idx, err := s.Repository.s.Index()
	c.Assert(err, IsNil)
	c.Assert(idx.Entries, HasLen, 9)
}

func (s *WorktreeSuite) TestCheckoutIndexMemory(c *C) {
	h, err := s.Repository.Head()
	c.Assert(err, IsNil)

	fs := memory.New()
	w := &Worktree{
		r:  s.Repository,
		fs: fs,
	}

	err = w.Checkout(h.Hash())
	c.Assert(err, IsNil)

	idx, err := s.Repository.s.Index()
	c.Assert(err, IsNil)
	c.Assert(idx.Entries, HasLen, 9)
	c.Assert(idx.Entries[0].Hash.String(), Equals, "32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	c.Assert(idx.Entries[0].Name, Equals, ".gitignore")
	// in memoryfs the perms are not supported
	c.Assert(idx.Entries[0].Mode, Equals, os.FileMode(0))
	c.Assert(idx.Entries[0].ModifiedAt.IsZero(), Equals, false)
	c.Assert(idx.Entries[0].Size, Equals, uint32(189))

	// ctime, dev, inode, uid and gid are not supported on memory fs
	c.Assert(idx.Entries[0].CreatedAt.IsZero(), Equals, true)
	c.Assert(idx.Entries[0].Dev, Equals, uint32(0))
	c.Assert(idx.Entries[0].Inode, Equals, uint32(0))
	c.Assert(idx.Entries[0].UID, Equals, uint32(0))
	c.Assert(idx.Entries[0].GID, Equals, uint32(0))
}

func (s *WorktreeSuite) TestCheckoutIndexOS(c *C) {
	h, err := s.Repository.Head()
	c.Assert(err, IsNil)

	dir, err := ioutil.TempDir("", "checkout")
	defer os.RemoveAll(dir)

	fs := osfs.New(dir)
	w := &Worktree{
		r:  s.Repository,
		fs: fs,
	}

	err = w.Checkout(h.Hash())
	c.Assert(err, IsNil)

	idx, err := s.Repository.s.Index()
	c.Assert(err, IsNil)
	c.Assert(idx.Entries, HasLen, 9)
	c.Assert(idx.Entries[0].Hash.String(), Equals, "32858aad3c383ed1ff0a0f9bdf231d54a00c9e88")
	c.Assert(idx.Entries[0].Name, Equals, ".gitignore")
	c.Assert(idx.Entries[0].Mode, Equals, os.FileMode(0644))
	c.Assert(idx.Entries[0].ModifiedAt.IsZero(), Equals, false)
	c.Assert(idx.Entries[0].Size, Equals, uint32(189))

	c.Assert(idx.Entries[0].CreatedAt.IsZero(), Equals, false)
	c.Assert(idx.Entries[0].Dev, Not(Equals), uint32(0))
	c.Assert(idx.Entries[0].Inode, Not(Equals), uint32(0))
	c.Assert(idx.Entries[0].UID, Not(Equals), uint32(0))
	c.Assert(idx.Entries[0].GID, Not(Equals), uint32(0))
}

func (s *WorktreeSuite) TestStatus(c *C) {

	h, err := s.Repository.Head()
	c.Assert(err, IsNil)

	fs := memory.New()
	w := &Worktree{
		r:  s.Repository,
		fs: fs,
	}

	err = w.Checkout(h.Hash())
	c.Assert(err, IsNil)

	status, err := w.Status()
	c.Assert(err, IsNil)

	fmt.Println(status)
	c.Assert(status.IsClean(), Equals, true)
}

func (s *WorktreeSuite) TestStatusModified(c *C) {
	c.Assert(s.Repository.s.SetIndex(&index.Index{Version: 2}), IsNil)

	h, err := s.Repository.Head()
	c.Assert(err, IsNil)

	dir, err := ioutil.TempDir("", "status")
	defer os.RemoveAll(dir)

	fs := osfs.New(dir)
	w := &Worktree{
		r:  s.Repository,
		fs: fs,
	}

	err = w.Checkout(h.Hash())
	c.Assert(err, IsNil)

	f, err := fs.Create(".gitignore")
	c.Assert(err, IsNil)
	_, err = f.Write([]byte("foo"))
	c.Assert(err, IsNil)
	err = f.Close()
	c.Assert(err, IsNil)

	status, err := w.Status()
	c.Assert(err, IsNil)
	c.Assert(status.IsClean(), Equals, false)
}
