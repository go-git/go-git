// +build norwfs

package dotgit

import "gopkg.in/src-d/go-git.v4/plumbing"

// There are some filesystems tha don't support opening files in RDWD mode.
// In these filesystems the standard SetRef function can not be used as i
// reads the reference file to check that it's not modified before updating it.
//
// This version of the function writes the reference without extra checks
// making it compatible with these simple filesystems. This is usually not
// a problem as they should be accessed by only one process at a time.
func (d *DotGit) setRef(fileName, content string, old *plumbing.Reference) error {
	f, err := d.fs.Create(fileName)
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = f.Write([]byte(content))
	return err
}
