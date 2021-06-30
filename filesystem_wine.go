package git

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/go-git/go-billy/v5"
)

var (
	errorWineSymlinkSyscallBroken = errors.New("Wine Symlink Syscall is broken")
)

type possibleWineSymlinkError struct {
	text string
	base error
}

func newPossibleWineSymlinkError(format string, a ...interface{}) *possibleWineSymlinkError {
	return &possibleWineSymlinkError{
		text: fmt.Sprintf(format, a...),
		base: errorWineSymlinkSyscallBroken,
	}
}

func (err *possibleWineSymlinkError) Error() string {
	return err.text
}

func (err *possibleWineSymlinkError) Unwrap() error {
	return err.base
}

type possibleWineFilesystem struct {
	// Filsystem implementation which might call into wine
	base billy.Filesystem
	// Used to check whether we need to execute Symlink validation
	hasCalledSymlinkSuccessfullyOnce bool
	defaultError                     error
}

func (fs *possibleWineFilesystem) Create(filename string) (billy.File, error) {
	return fs.base.Create(filename)
}

func (fs *possibleWineFilesystem) Open(filename string) (billy.File, error) {
	return fs.base.Open(filename)
}

func (fs *possibleWineFilesystem) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	return fs.base.OpenFile(filename, flag, perm)
}

func (fs *possibleWineFilesystem) Stat(filename string) (os.FileInfo, error) {
	return fs.base.Stat(filename)
}

func (fs *possibleWineFilesystem) Rename(oldpath, newpath string) error {
	return fs.base.Rename(oldpath, newpath)
}

func (fs *possibleWineFilesystem) Remove(filename string) error {
	return fs.base.Remove(filename)
}

func (fs *possibleWineFilesystem) Join(elem ...string) string {
	return fs.base.Join(elem...)
}

func (fs *possibleWineFilesystem) TempFile(dir, prefix string) (billy.File, error) {
	return fs.base.TempFile(dir, prefix)
}

func (fs *possibleWineFilesystem) ReadDir(path string) ([]os.FileInfo, error) {
	return fs.base.ReadDir(path)
}

func (fs *possibleWineFilesystem) MkdirAll(filename string, perm os.FileMode) error {
	return fs.base.MkdirAll(filename, perm)
}

func (fs *possibleWineFilesystem) Lstat(filename string) (os.FileInfo, error) {
	return fs.base.Lstat(filename)
}

func (fs *possibleWineFilesystem) Symlink(target, link string) error {

	err := fs.base.Symlink(target, link)

	if err != nil {
		return err
	}

	// Due to Wine Bug we have to paranoia
	// check whether the Symlink was actually
	// created

	if fs.hasCalledSymlinkSuccessfullyOnce {
		// Reuse the result of the first call
		return fs.defaultError
	}

	fs.hasCalledSymlinkSuccessfullyOnce = true

	// We need to check whether Wine might have lied to us
	_, err = fs.Readlink(link)
	if err == nil {
		// Everything seems fine
		return nil
	}

	fs.defaultError = newPossibleWineSymlinkError("Wine detection triggered for: %s. Either your Symlink was immediately removed or you are running Wine with a broken Symlink syscall.", link)
	return fs.defaultError
}

func (fs *possibleWineFilesystem) Readlink(link string) (string, error) {
	return fs.base.Readlink(link)
}

func (fs *possibleWineFilesystem) Chroot(path string) (billy.Filesystem, error) {
	return fs.base.Chroot(path)
}

func (fs *possibleWineFilesystem) Root() string {
	return fs.base.Root()
}

// Wine Symlink syscalls are stubs. Sadly they are also broken in that they
// always return TRUE. Thus for the first Symlink call check whether
// the base implementation really did work or was lying.
// If it was lying turn on "Wine" mode.
func handleBrokenWineSymlinkStub(worktree billy.Filesystem) billy.Filesystem {
	if worktree == nil {
		return nil
	}

	if runtime.GOOS == "windows" {
		// This might be Wine
		return &possibleWineFilesystem{
			base: worktree,
		}
	}
	return worktree
}
