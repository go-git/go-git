package fixtures

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"

	"github.com/alcortesm/tgz"

	"gopkg.in/check.v1"
	"gopkg.in/src-d/go-git.v4/core"
	"gopkg.in/src-d/go-git.v4/utils/fs"
)

var RootFolder = ""

const DataFolder = "data"

var folders []string

var fixtures = Fixtures{{
	Tags:         []string{"packfile", "ofs-delta", ".git"},
	URL:          "https://github.com/git-fixtures/basic.git",
	Head:         core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	PackfileHash: core.NewHash("a3fed42da1e8189a077c0e6846c040dcf73fc9dd"),
	DotGitHash:   core.NewHash("0a00a25543e6d732dbf4e8e9fec55c8e65fc4e8d"),
	ObjectsCount: 31,
}, {
	Tags:         []string{"packfile", "ref-delta", ".git"},
	URL:          "https://github.com/git-fixtures/basic.git",
	Head:         core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	PackfileHash: core.NewHash("c544593473465e6315ad4182d04d366c4592b829"),
	DotGitHash:   core.NewHash("7cbde0ca02f13aedd5ec8b358ca17b1c0bf5ee64"),
	ObjectsCount: 31,
}, {
	Tags:         []string{"packfile", "ofs-delta", ".git", "single-branch"},
	URL:          "https://github.com/git-fixtures/basic.git",
	Head:         core.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	PackfileHash: core.NewHash("61f0ee9c75af1f9678e6f76ff39fbe372b6f1c45"),
	DotGitHash:   core.NewHash("21504f6d2cc2ef0c9d6ebb8802c7b49abae40c1a"),
	ObjectsCount: 28,
}, {
	Tags:         []string{"packfile", ".git", "unpacked", "multi-packfile"},
	URL:          "https://github.com/src-d/go-git.git",
	Head:         core.NewHash("e8788ad9165781196e917292d6055cba1d78664e"),
	PackfileHash: core.NewHash("3559b3b47e695b33b0913237a4df3357e739831c"),
	DotGitHash:   core.NewHash("174be6bd4292c18160542ae6dc6704b877b8a01a"),
	ObjectsCount: 2133,
}, {
	Tags:         []string{"packfile", "tags"},
	URL:          "https://github.com/git-fixtures/tags.git",
	Head:         core.NewHash("f7b877701fbf855b44c0a9e86f3fdce2c298b07f"),
	PackfileHash: core.NewHash("b68617dd8637fe6409d9842825a843a1d9a6e484"),
}, {
	Tags:         []string{"packfile"},
	URL:          "https://github.com/spinnaker/spinnaker.git",
	Head:         core.NewHash("06ce06d0fc49646c4de733c45b7788aabad98a6f"),
	PackfileHash: core.NewHash("f2e0a8889a746f7600e07d2246a2e29a72f696be"),
}, {
	Tags:         []string{"packfile"},
	URL:          "https://github.com/jamesob/desk.git",
	Head:         core.NewHash("d2313db6e7ca7bac79b819d767b2a1449abb0a5d"),
	PackfileHash: core.NewHash("4ec6344877f494690fc800aceaf2ca0e86786acb"),
}, {
	Tags:         []string{"packfile", "empty-folder"},
	URL:          "https://github.com/cpcs499/Final_Pres_P.git",
	Head:         core.NewHash("70bade703ce556c2c7391a8065c45c943e8b6bc3"),
	PackfileHash: core.NewHash("29f304662fd64f102d94722cf5bd8802d9a9472c"),
}}

func All() Fixtures {
	return fixtures
}

func Basic() Fixtures {
	return ByURL("https://github.com/git-fixtures/basic.git").
		Exclude("single-branch")
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

func (f *Fixture) Packfile() *os.File {
	fn := filepath.Join(RootFolder, DataFolder, fmt.Sprintf("pack-%s.pack", f.PackfileHash))
	file, err := os.Open(fn)
	if err != nil {
		panic(err)
	}

	return file
}

func (f *Fixture) Idx() *os.File {
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

func (g Fixtures) Exclude(tag string) Fixtures {
	r := make(Fixtures, 0)
	for _, f := range g {
		if !f.Is(tag) {
			r = append(r, f)
		}
	}

	return r
}

type Suite struct{}

func (s *Suite) SetUpSuite(c *check.C) {
	RootFolder = filepath.Join(
		build.Default.GOPATH,
		"src", "gopkg.in/src-d/go-git.v4", "fixtures",
	)
}

func (s *Suite) TearDownSuite(c *check.C) {
	for _, f := range folders {
		err := os.RemoveAll(f)
		c.Assert(err, check.IsNil)
	}
}
