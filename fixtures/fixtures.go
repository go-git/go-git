package fixtures

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alcortesm/tgz"

	check "gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

var RootFolder = ""

const DataFolder = "data"

var folders []string

var fixtures = Fixtures{{
	Tags:         []string{"packfile", "ofs-delta", ".git"},
	URL:          "https://github.com/git-fixtures/basic",
	Head:         core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	PackfileHash: core.NewHash("a3fed42da1e8189a077c0e6846c040dcf73fc9dd"),
	DotGitHash:   core.NewHash("0a00a25543e6d732dbf4e8e9fec55c8e65fc4e8d"),
	ObjectsCount: 31,
}, {
	Tags:         []string{"packfile", "ref-delta", ".git"},
	URL:          "https://github.com/git-fixtures/basic",
	Head:         core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	PackfileHash: core.NewHash("c544593473465e6315ad4182d04d366c4592b829"),
	DotGitHash:   core.NewHash("7cbde0ca02f13aedd5ec8b358ca17b1c0bf5ee64"),
	ObjectsCount: 31,
}, {
	Tags:         []string{".git", "unpacked", "multi-packfile"},
	URL:          "https://github.com/src-d/go-git.git",
	DotGitHash:   core.NewHash("174be6bd4292c18160542ae6dc6704b877b8a01a"),
	ObjectsCount: 2133,
}, {
	Tags:         []string{"packfile"},
	URL:          "https://github.com/spinnaker/spinnaker",
	Head:         core.NewHash("06ce06d0fc49646c4de733c45b7788aabad98a6f"),
	PackfileHash: core.NewHash("f2e0a8889a746f7600e07d2246a2e29a72f696be"),
}}

func All() Fixtures {
	return fixtures
}

func Basic() Fixtures {
	return ByURL("https://github.com/git-fixtures/basic")
}

func ByURL(url string) Fixtures {
	return fixtures.ByURL(url)
}

func ByTag(tag string) Fixtures {
	return fixtures.ByTag(tag)
}

type Fixture struct {
	URL          string
	Tags         []string
	Head         core.Hash
	PackfileHash core.Hash
	DotGitHash   core.Hash
	ObjectsCount int32
}

func (f *Fixture) Is(tag string) bool {
	for _, t := range f.Tags {
		if t == tag {
			return true
		}
	}

	return false
}

func (f *Fixture) Packfile() io.ReadSeeker {
	fn := filepath.Join(RootFolder, DataFolder, fmt.Sprintf("pack-%s.pack", f.PackfileHash))
	file, err := os.Open(fn)
	if err != nil {
		panic(err)
	}

	return file
}

func (f *Fixture) Idx() io.ReadSeeker {
	fn := filepath.Join(RootFolder, DataFolder, fmt.Sprintf("pack-%s.idx", f.PackfileHash))
	file, err := os.Open(fn)
	if err != nil {
		panic(err)
	}

	return file
}

func (f *Fixture) DotGit() fs.Filesystem {
	fn := filepath.Join(RootFolder, DataFolder, fmt.Sprintf("git-%s.tgz", f.DotGitHash))
	path, err := tgz.Extract(fn)
	if err != nil {
		panic(err)
	}

	folders = append(folders, path)
	return fs.NewOS(path)
}

type Fixtures []*Fixture

func (g Fixtures) Test(c *check.C, test func(*Fixture)) {
	for _, f := range g {
		c.Logf("executing test at %s %s", f.URL, f.Tags)
		test(f)
	}
}

func (g Fixtures) One() *Fixture {
	return g[0]
}

func (g Fixtures) ByTag(tag string) Fixtures {
	r := make(Fixtures, 0)
	for _, f := range g {
		if f.Is(tag) {
			r = append(r, f)
		}
	}

	return r
}
func (g Fixtures) ByURL(url string) Fixtures {
	r := make(Fixtures, 0)
	for _, f := range g {
		if f.URL == url {
			r = append(r, f)
		}
	}

	return r
}
