package dotgit

import (
	"errors"
	"io"
	"os"
	"runtime"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/utils/ioutil"
)

func (d *DotGit) openAndLockPackedRefsMode() int {
	if billy.CapabilityCheck(d.fs, billy.ReadAndWriteCapability) {
		return os.O_RDWR
	}

	return os.O_RDONLY
}

func (d *DotGit) rewritePackedRefsWhileLocked(
	tmp, pr billy.File,
) error {
	// Try plain rename. If we aren't using the bare Windows filesystem as the
	// storage layer, we might be able to get away with a rename over a locked
	// file.
	err := d.fs.Rename(tmp.Name(), pr.Name())
	if err == nil {
		return nil
	}

	// If we are in a filesystem that does not support rename (e.g. sivafs)
	// a full copy is done.
	if errors.Is(err, billy.ErrNotSupported) {
		return d.copyNewFile(tmp, pr)
	}

	if runtime.GOOS != "windows" {
		return err
	}

	// Otherwise, Windows doesn't let us rename over a locked file, so
	// we have to do a straight copy.  Unfortunately this could result
	// in a partially-written file if the process fails before the
	// copy completes.
	return d.copyToExistingFile(tmp, pr)
}

func (d *DotGit) copyToExistingFile(tmp, pr billy.File) error {
	_, err := pr.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	err = pr.Truncate(0)
	if err != nil {
		return err
	}
	_, err = tmp.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	_, err = ioutil.CopyBufferPool(pr, tmp)

	return err
}

func (d *DotGit) copyNewFile(tmp, pr billy.File) (err error) {
	prWrite, err := d.fs.Create(pr.Name())
	if err != nil {
		return err
	}

	defer ioutil.CheckClose(prWrite, &err)

	_, err = tmp.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = ioutil.CopyBufferPool(prWrite, tmp)

	return err
}
