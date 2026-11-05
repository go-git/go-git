//go:build unix

package dotgit

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
)

// TestModuleNestingWithNamedPipe covers a named pipe in place of a file read
// while checking for nesting. Opening one waits for a writer, so a repository
// could otherwise stall the caller for as long as it is held open.
func (s *SuiteDotGit) TestModuleNestingWithNamedPipe() {
	tests := []struct {
		name    string
		file    string
		wantErr bool
	}{
		{name: "HEAD", file: "HEAD"},
		{name: "commondir", file: "commondir", wantErr: true},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			root := s.T().TempDir()
			module := filepath.Join(root, "modules", "lib")
			s.Require().NoError(os.MkdirAll(module, 0o755))
			s.Require().NoError(syscall.Mkfifo(filepath.Join(module, tc.file), 0o644))

			fs := osfs.New(root)
			if tc.file != "HEAD" {
				// commondir is only read once HEAD is recognized.
				s.Require().NoError(util.WriteFile(fs, "modules/lib/HEAD", []byte("ref: refs/heads/master\n"), 0o644))
			}

			done := make(chan error, 1)
			go func() { _, err := New(fs).Module("lib/refs/heads"); done <- err }()

			select {
			case err := <-done:
				if tc.wantErr {
					s.Error(err)
				} else {
					s.NoError(err)
				}
			case <-time.After(10 * time.Second):
				s.Fail("Module did not return", "blocked reading the named pipe %q", tc.file)
			}
		})
	}
}

// TestModuleNestingWithUnreadableCommonDirComponent covers a commondir path
// with a component that cannot be examined for a reason other than being
// absent. Git's realpath fails there; applying the ".." that follows it
// lexically could find a Git directory Git does not.
func (s *SuiteDotGit) TestModuleNestingWithUnreadableCommonDirComponent() {
	if os.Geteuid() == 0 {
		s.T().Skip("permissions do not apply to root")
	}

	root := s.T().TempDir()
	fs := osfs.New(root)
	common, err := fs.Chroot("common")
	s.Require().NoError(err)
	s.Require().NoError(New(common).Initialize())
	s.Require().NoError(util.WriteFile(fs, "modules/lib/HEAD", []byte("ref: refs/heads/master\n"), 0o644))
	s.Require().NoError(util.WriteFile(fs, "modules/lib/commondir", []byte("locked/x/../../../../common\n"), 0o644))
	locked := filepath.Join(root, "modules", "lib", "locked")
	s.Require().NoError(os.Mkdir(locked, 0o755))
	s.Require().NoError(os.Chmod(locked, 0))
	s.T().Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err = New(fs).Module("lib/refs/heads")
	s.Require().ErrorIs(err, os.ErrPermission)
	s.NotErrorIs(err, ErrModuleGitDirNested)
}

// TestModuleNestingWithEntriesLinkedOutsideRoot covers a prefix whose
// objects and refs are symlinks leaving the filesystem root. Git's access
// follows them and finds the prefix a Git directory, so the nesting must not
// be accepted where the filesystem refuses to follow them.
func (s *SuiteDotGit) TestModuleNestingWithEntriesLinkedOutsideRoot() {
	for _, tc := range []struct {
		name string
		opts []osfs.Option
	}{
		{name: "default"},
		{name: "BoundOS", opts: []osfs.Option{osfs.WithBoundOS()}},
	} {
		s.Run(tc.name, func() {
			outside := s.T().TempDir()
			for _, dir := range []string{"objects", "refs"} {
				s.Require().NoError(os.Mkdir(filepath.Join(outside, dir), 0o755))
			}

			root := s.T().TempDir()
			module := filepath.Join(root, "modules", "lib")
			s.Require().NoError(os.MkdirAll(module, 0o755))
			s.Require().NoError(os.WriteFile(filepath.Join(module, "HEAD"), []byte("ref: refs/heads/master\n"), 0o644))
			for _, dir := range []string{"objects", "refs"} {
				s.Require().NoError(os.Symlink(filepath.Join(outside, dir), filepath.Join(module, dir)))
			}

			_, err := New(osfs.New(root, tc.opts...)).Module("lib/hooks")
			s.Require().Error(err)
			s.NotErrorIs(err, ErrModuleGitDirNested)
		})
	}
}
