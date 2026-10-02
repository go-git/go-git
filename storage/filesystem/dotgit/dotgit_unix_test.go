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
