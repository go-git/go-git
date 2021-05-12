package dotgit

import (
	"os"

	. "gopkg.in/check.v1"
)

func (s *SuiteDotGit) TestRepositoryFilesystem(c *C) {
	fs, clean := s.TemporalFilesystem()
	defer clean()

	err := fs.MkdirAll("dotGit", 0777)
	c.Assert(err, IsNil)
	dotGitFs, err := fs.Chroot("dotGit")
	c.Assert(err, IsNil)

	err = fs.MkdirAll("commonDotGit", 0777)
	c.Assert(err, IsNil)
	commonDotGitFs, err := fs.Chroot("commonDotGit")
	c.Assert(err, IsNil)

	repositoryFs := NewRepositoryFilesystem(dotGitFs, commonDotGitFs)
	c.Assert(repositoryFs.Root(), Equals, dotGitFs.Root())

	somedir, err := repositoryFs.Chroot("somedir")
	c.Assert(err, IsNil)
	c.Assert(somedir.Root(), Equals, repositoryFs.Join(dotGitFs.Root(), "somedir"))

	_, err = repositoryFs.Create("somefile")
	c.Assert(err, IsNil)

	_, err = repositoryFs.Stat("somefile")
	c.Assert(err, IsNil)

	file, err := repositoryFs.Open("somefile")
	c.Assert(err, IsNil)
	err = file.Close()
	c.Assert(err, IsNil)

	file, err = repositoryFs.OpenFile("somefile", os.O_RDONLY, 0666)
	c.Assert(err, IsNil)
	err = file.Close()
	c.Assert(err, IsNil)

	file, err = repositoryFs.Create("somefile2")
	c.Assert(err, IsNil)
	err = file.Close()
	c.Assert(err, IsNil)
	_, err = repositoryFs.Stat("somefile2")
	c.Assert(err, IsNil)
	err = repositoryFs.Rename("somefile2", "newfile")
	c.Assert(err, IsNil)

	tempDir, err := repositoryFs.TempFile("tmp", "myprefix")
	c.Assert(err, IsNil)
	c.Assert(repositoryFs.Join(repositoryFs.Root(), "tmp", tempDir.Name()), Equals, repositoryFs.Join(dotGitFs.Root(), "tmp", tempDir.Name()))

	err = repositoryFs.Symlink("newfile", "somelink")
	c.Assert(err, IsNil)

	_, err = repositoryFs.Lstat("somelink")
	c.Assert(err, IsNil)

	link, err := repositoryFs.Readlink("somelink")
	c.Assert(err, IsNil)
	c.Assert(link, Equals, "newfile")

	err = repositoryFs.Remove("somelink")
	c.Assert(err, IsNil)

	_, err = repositoryFs.Stat("somelink")
	c.Assert(os.IsNotExist(err), Equals, true)

	dirs := []string{objectsPath, refsPath, packedRefsPath, configPath, branchesPath, hooksPath, infoPath, remotesPath, logsPath, shallowPath, worktreesPath}
	for _, dir := range dirs {
		err := repositoryFs.MkdirAll(dir, 0777)
		c.Assert(err, IsNil)
		_, err = commonDotGitFs.Stat(dir)
		c.Assert(err, IsNil)
		_, err = dotGitFs.Stat(dir)
		c.Assert(os.IsNotExist(err), Equals, true)
	}

	exceptionsPaths := []string{repositoryFs.Join(logsPath, "HEAD"), repositoryFs.Join(refsPath, "bisect"), repositoryFs.Join(refsPath, "rewritten"), repositoryFs.Join(refsPath, "worktree")}
	for _, path := range exceptionsPaths {
		_, err := repositoryFs.Create(path)
		c.Assert(err, IsNil)
		_, err = commonDotGitFs.Stat(path)
		c.Assert(os.IsNotExist(err), Equals, true)
		_, err = dotGitFs.Stat(path)
		c.Assert(err, IsNil)
	}

	err = repositoryFs.MkdirAll("refs/heads", 0777)
	c.Assert(err, IsNil)
	_, err = commonDotGitFs.Stat("refs/heads")
	c.Assert(err, IsNil)
	_, err = dotGitFs.Stat("refs/heads")
	c.Assert(os.IsNotExist(err), Equals, true)

	err = repositoryFs.MkdirAll("objects/pack", 0777)
	c.Assert(err, IsNil)
	_, err = commonDotGitFs.Stat("objects/pack")
	c.Assert(err, IsNil)
	_, err = dotGitFs.Stat("objects/pack")
	c.Assert(os.IsNotExist(err), Equals, true)

	err = repositoryFs.MkdirAll("a/b/c", 0777)
	c.Assert(err, IsNil)
	_, err = commonDotGitFs.Stat("a/b/c")
	c.Assert(os.IsNotExist(err), Equals, true)
	_, err = dotGitFs.Stat("a/b/c")
	c.Assert(err, IsNil)
}
