package repository

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/internal/path_util"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

var ErrCommonDirNotFound = errors.New("commondir not found")

func PlainOpen(path string, detectDotGit bool, enableDotGitCommonDir bool) (*filesystem.Storage, billy.Filesystem, error) {
	dot, wt, err := DotGitToOSFilesystems(path, detectDotGit)
	if err != nil {
		return nil, nil, err
	}

	if _, err := dot.Stat(""); err != nil {
		return nil, nil, err
	}

	var repositoryFs billy.Filesystem

	if enableDotGitCommonDir {
		dotGitCommon, err := DotGitCommonDirectory(dot)
		if err != nil {
			return nil, nil, err
		}
		repositoryFs = dotgit.NewRepositoryFilesystem(dot, dotGitCommon)
	} else {
		repositoryFs = dot
	}

	s := filesystem.NewStorage(repositoryFs, cache.NewObjectLRUDefault())

	return s, wt, nil
}

func DotGitToOSFilesystems(path string, detect bool) (dot, wt billy.Filesystem, err error) {
	path, err = path_util.ReplaceTildeWithHome(path)
	if err != nil {
		return nil, nil, err
	}

	if path, err = filepath.Abs(path); err != nil {
		return nil, nil, err
	}

	var fs billy.Filesystem
	var fi os.FileInfo
	for {
		fs = osfs.New(path)

		pathinfo, err := fs.Stat("/")
		if !os.IsNotExist(err) {
			if pathinfo == nil {
				return nil, nil, err
			}
			if !pathinfo.IsDir() && detect {
				fs = osfs.New(filepath.Dir(path))
			}
		}

		fi, err = fs.Stat(".git")
		if err == nil {
			// no error; stop
			break
		}
		if !os.IsNotExist(err) {
			// unknown error; stop
			return nil, nil, err
		}
		if detect {
			// try its parent as long as we haven't reached
			// the root dir
			if dir := filepath.Dir(path); dir != path {
				path = dir
				continue
			}
		}
		// not detecting via parent dirs and the dir does not exist;
		// stop
		return fs, nil, nil
	}

	if fi.IsDir() {
		dot, err = fs.Chroot(".git")
		return dot, fs, err
	}

	dot, err = DotGitFileToOSFilesystem(path, fs)
	if err != nil {
		return nil, nil, err
	}

	return dot, fs, nil
}

func DotGitFileToOSFilesystem(path string, fs billy.Filesystem) (bfs billy.Filesystem, err error) {
	f, err := fs.Open(".git")
	if err != nil {
		return nil, err
	}
	defer ioutil.CheckClose(f, &err)

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	line := string(b)
	const prefix = "gitdir: "
	if !strings.HasPrefix(line, prefix) {
		return nil, fmt.Errorf(".git file has no %s prefix", prefix)
	}

	gitdir := strings.Split(line[len(prefix):], "\n")[0]
	gitdir = strings.TrimSpace(gitdir)
	if filepath.IsAbs(gitdir) {
		return osfs.New(gitdir), nil
	}

	return osfs.New(fs.Join(path, gitdir)), nil
}

func DotGitCommonDirectory(fs billy.Filesystem) (commonDir billy.Filesystem, err error) {
	f, err := fs.Open("commondir")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		path := strings.TrimSpace(string(b))
		if filepath.IsAbs(path) {
			commonDir = osfs.New(path)
		} else {
			commonDir = osfs.New(filepath.Join(fs.Root(), path))
		}
		if _, err := commonDir.Stat(""); err != nil {
			if os.IsNotExist(err) {
				return nil, ErrCommonDirNotFound
			}

			return nil, err
		}
	}

	return commonDir, nil
}
