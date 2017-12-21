// +build norwfs

package dotgit

import "gopkg.in/src-d/go-git.v4/plumbing"

func (d *DotGit) setRef(fileName, content string, old *plumbing.Reference) error {
	f, err := d.fs.Create(fileName)
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = f.Write([]byte(content))
	return err
}
