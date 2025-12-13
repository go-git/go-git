package dotgit

import (
	"os"
)

func (s *SuiteDotGit) TestRepositoryFilesystem() {
	fs := s.EmptyFS()

	err := fs.MkdirAll("dotGit", 0o777)
	s.Require().NoError(err)
	dotGitFs, err := fs.Chroot("dotGit")
	s.Require().NoError(err)

	err = fs.MkdirAll("commonDotGit", 0o777)
	s.Require().NoError(err)
	commonDotGitFs, err := fs.Chroot("commonDotGit")
	s.Require().NoError(err)

	repositoryFs := NewRepositoryFilesystem(dotGitFs, commonDotGitFs)
	s.Equal(dotGitFs.Root(), repositoryFs.Root())

	somedir, err := repositoryFs.Chroot("somedir")
	s.Require().NoError(err)
	s.Equal(repositoryFs.Join(dotGitFs.Root(), "somedir"), somedir.Root())

	_, err = repositoryFs.Create("somefile")
	s.Require().NoError(err)

	_, err = repositoryFs.Stat("somefile")
	s.Require().NoError(err)

	file, err := repositoryFs.Open("somefile")
	s.Require().NoError(err)
	err = file.Close()
	s.Require().NoError(err)

	file, err = repositoryFs.OpenFile("somefile", os.O_RDONLY, 0o666)
	s.Require().NoError(err)
	err = file.Close()
	s.Require().NoError(err)

	file, err = repositoryFs.Create("somefile2")
	s.Require().NoError(err)
	err = file.Close()
	s.Require().NoError(err)
	_, err = repositoryFs.Stat("somefile2")
	s.Require().NoError(err)
	err = repositoryFs.Rename("somefile2", "newfile")
	s.Require().NoError(err)

	tempDir, err := repositoryFs.TempFile("tmp", "myprefix")
	s.Require().NoError(err)
	s.Equal(repositoryFs.Join(dotGitFs.Root(), "tmp", tempDir.Name()), repositoryFs.Join(repositoryFs.Root(), "tmp", tempDir.Name()))

	err = repositoryFs.Symlink("newfile", "somelink")
	s.Require().NoError(err)

	_, err = repositoryFs.Lstat("somelink")
	s.Require().NoError(err)

	link, err := repositoryFs.Readlink("somelink")
	s.Require().NoError(err)
	s.Equal("newfile", link)

	err = repositoryFs.Remove("somelink")
	s.Require().NoError(err)

	_, err = repositoryFs.Stat("somelink")
	s.True(os.IsNotExist(err))

	dirs := []string{objectsPath, refsPath, packedRefsPath, configPath, branchesPath, hooksPath, infoPath, remotesPath, logsPath, shallowPath, worktreesPath}
	for _, dir := range dirs {
		err := repositoryFs.MkdirAll(dir, 0o777)
		s.Require().NoError(err)
		_, err = commonDotGitFs.Stat(dir)
		s.Require().NoError(err)
		_, err = dotGitFs.Stat(dir)
		s.True(os.IsNotExist(err))
	}

	exceptionsPaths := []string{repositoryFs.Join(logsPath, "HEAD"), repositoryFs.Join(refsPath, "bisect"), repositoryFs.Join(refsPath, "rewritten"), repositoryFs.Join(refsPath, "worktree")}
	for _, path := range exceptionsPaths {
		_, err := repositoryFs.Create(path)
		s.Require().NoError(err)
		_, err = commonDotGitFs.Stat(path)
		s.True(os.IsNotExist(err))
		_, err = dotGitFs.Stat(path)
		s.Require().NoError(err)
	}

	err = repositoryFs.MkdirAll("refs/heads", 0o777)
	s.Require().NoError(err)
	_, err = commonDotGitFs.Stat("refs/heads")
	s.Require().NoError(err)
	_, err = dotGitFs.Stat("refs/heads")
	s.True(os.IsNotExist(err))

	err = repositoryFs.MkdirAll("objects/pack", 0o777)
	s.Require().NoError(err)
	_, err = commonDotGitFs.Stat("objects/pack")
	s.Require().NoError(err)
	_, err = dotGitFs.Stat("objects/pack")
	s.True(os.IsNotExist(err))

	err = repositoryFs.MkdirAll("a/b/c", 0o777)
	s.Require().NoError(err)
	_, err = commonDotGitFs.Stat("a/b/c")
	s.True(os.IsNotExist(err))
	_, err = dotGitFs.Stat("a/b/c")
	s.Require().NoError(err)
}

// TestRepositoryFilesystemTempFileRename tests the TempFile + Rename flow
// which is used by ObjectWriter.save(). This is critical for worktree support
// where temp files are created in commonDotGitFs.
func (s *SuiteDotGit) TestRepositoryFilesystemTempFileRename() {
	fs := s.EmptyFS()

	err := fs.MkdirAll("dotGit", 0o777)
	s.Require().NoError(err)
	dotGitFs, err := fs.Chroot("dotGit")
	s.Require().NoError(err)

	err = fs.MkdirAll("commonDotGit", 0o777)
	s.Require().NoError(err)
	commonDotGitFs, err := fs.Chroot("commonDotGit")
	s.Require().NoError(err)

	repositoryFs := NewRepositoryFilesystem(dotGitFs, commonDotGitFs)

	// Create objects/pack directory in commonDotGitFs
	err = repositoryFs.MkdirAll("objects/pack", 0o777)
	s.Require().NoError(err)

	// Create a temp file in objects/pack (should go to commonDotGitFs)
	tempFile, err := repositoryFs.TempFile("objects/pack", "tmp_obj_")
	s.Require().NoError(err)
	tempFilePath := tempFile.Name()
	err = tempFile.Close()
	s.Require().NoError(err)

	// Create target directory for rename
	err = repositoryFs.MkdirAll("objects/ab", 0o777)
	s.Require().NoError(err)

	// Rename using the temp file path should work
	// This simulates what ObjectWriter.save() does
	err = repositoryFs.Rename(tempFilePath, "objects/ab/cdef1234")
	s.Require().NoError(err, "rename should work")

	// Verify the file was renamed in commonDotGitFs
	_, err = commonDotGitFs.Stat("objects/ab/cdef1234")
	s.Require().NoError(err, "renamed file should exist in commonDotGitFs")

	// Verify the file doesn't exist in dotGitFs
	_, err = dotGitFs.Stat("objects/ab/cdef1234")
	s.True(os.IsNotExist(err), "renamed file should NOT exist in dotGitFs")
}

// TestRepositoryFilesystemTempFileRenameToDotGitFs tests the TempFile + Rename flow
// for paths that route to dotGitFs (worktree-specific, non-commondir paths).
func (s *SuiteDotGit) TestRepositoryFilesystemTempFileRenameToDotGitFs() {
	fs := s.EmptyFS()

	err := fs.MkdirAll("dotGit", 0o777)
	s.Require().NoError(err)
	dotGitFs, err := fs.Chroot("dotGit")
	s.Require().NoError(err)

	err = fs.MkdirAll("commonDotGit", 0o777)
	s.Require().NoError(err)
	commonDotGitFs, err := fs.Chroot("commonDotGit")
	s.Require().NoError(err)

	repositoryFs := NewRepositoryFilesystem(dotGitFs, commonDotGitFs)

	// Create a worktree-specific directory (non-commondir paths go to dotGitFs)
	err = repositoryFs.MkdirAll("worktree-data", 0o777)
	s.Require().NoError(err)

	// Verify directory was created in dotGitFs
	_, err = dotGitFs.Stat("worktree-data")
	s.Require().NoError(err, "worktree-data should exist in dotGitFs")

	// Create a temp file in worktree-data (should go to dotGitFs)
	tempFile, err := repositoryFs.TempFile("worktree-data", "tmp_wt_")
	s.Require().NoError(err)
	tempFilePath := tempFile.Name()
	err = tempFile.Close()
	s.Require().NoError(err)

	// Rename using the temp file path (absolute path from dotGitFs)
	err = repositoryFs.Rename(tempFilePath, "worktree-data/renamed-file")
	s.Require().NoError(err, "rename should work for dotGitFs paths")

	// Verify the file was renamed in dotGitFs
	_, err = dotGitFs.Stat("worktree-data/renamed-file")
	s.Require().NoError(err, "renamed file should exist in dotGitFs")

	// Verify the file doesn't exist in commonDotGitFs
	_, err = commonDotGitFs.Stat("worktree-data/renamed-file")
	s.True(os.IsNotExist(err), "renamed file should NOT exist in commonDotGitFs")
}

// TestRepositoryFilesystemAbsolutePathFallback tests that absolute paths not matching
// either filesystem root default to dotGitFs.
func (s *SuiteDotGit) TestRepositoryFilesystemAbsolutePathFallback() {
	fs := s.EmptyFS()

	err := fs.MkdirAll("dotGit", 0o777)
	s.Require().NoError(err)
	dotGitFs, err := fs.Chroot("dotGit")
	s.Require().NoError(err)

	err = fs.MkdirAll("commonDotGit", 0o777)
	s.Require().NoError(err)
	commonDotGitFs, err := fs.Chroot("commonDotGit")
	s.Require().NoError(err)

	repositoryFs := NewRepositoryFilesystem(dotGitFs, commonDotGitFs)

	// Test that mapToRepositoryFsByPath returns dotGitFs for unmatched absolute paths
	// We can't directly test mapToRepositoryFsByPath, but we can verify behavior
	// by checking that operations on arbitrary paths go to dotGitFs

	err = repositoryFs.MkdirAll("arbitrary/path", 0o777)
	s.Require().NoError(err)

	// Verify it went to dotGitFs (default for non-commondir paths)
	_, err = dotGitFs.Stat("arbitrary/path")
	s.Require().NoError(err, "arbitrary path should exist in dotGitFs")

	_, err = commonDotGitFs.Stat("arbitrary/path")
	s.True(os.IsNotExist(err), "arbitrary path should NOT exist in commonDotGitFs")
}
