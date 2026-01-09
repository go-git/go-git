package pack_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
)

type dataSource struct {
	name          string
	files         func() (billy.File, billy.File, billy.File)
	run           bool
	offsetHashMap map[uint64]string
}

func dataSources(tb testing.TB) []dataSource {
	d := []dataSource{{
		name: "basic-fixture",
		files: func() (billy.File, billy.File, billy.File) {
			fixture := fixtures.NewOSFixture(fixtures.Basic().One(), tb.TempDir())
			return fixture.Packfile(), fixture.Idx(), fixture.Rev()
		},
		run: true,
		offsetHashMap: map[uint64]string{
			186:   "6ecf0ef2c2dffb796033e5a02219af86ec6584e5",
			1524:  "32858aad3c383ed1ff0a0f9bdf231d54a00c9e88",
			84375: "a8d315b2b1c615d43042c3a62402b8a54288cf5c",
			84760: "aa9b383c260e1d05fbbf6b30a02914555e20c725",
		},
	}, {
		name: "linux-kernel",
		files: func() (billy.File, billy.File, billy.File) {
			matches, err := filepath.Glob("../testdata/repos/linux/.git/objects/pack/pack-*.pack")
			if err != nil || len(matches) == 0 {
				tb.Fatal("cannot find pack file")
			}

			pack, err := os.Open(matches[0])
			require.NoError(tb, err)

			idx, err := os.Open(strings.TrimSuffix(matches[0], "pack") + "idx")
			require.NoError(tb, err)

			rev, err := os.Open(strings.TrimSuffix(matches[0], "pack") + "rev")
			require.NoError(tb, err)

			return pack, idx, rev
		},
		run: func() bool {
			_, err := os.Stat("../testdata/repos/linux")
			return err == nil
		}(),
		offsetHashMap: map[uint64]string{
			4699660837: "00000264228858f73a003e22cb157df1634519cb",
			4107694716: "00000474c3ea8b55a762db8ade76e2c2c18cd9b6",
			5619108999: "38041673411cf517dc41a60041bf48864c06d988",
			5738050296: "ffaf8dddd5f9c6df5dbd68032f078a7d02762f1f",
		},
	}}
	return d
}

func BenchmarkPackHandlers(b *testing.B) {
	for _, data := range dataSources(b) {
		if !data.run {
			continue
		}

		pack, idx, rev := data.files()
		b.Cleanup(func() {
			pack.Close()
			idx.Close()
			rev.Close()
		})

		runBenchmark(b, "packfile-cache-osfs: "+data.name, func() packHandler[int64] {
			return newPackfileOpts(pack, idx,
				packfile.WithFs(osfs.New(b.TempDir())),
				packfile.WithCache(cache.NewObjectLRUDefault()))
		}, data.offsetHashMap)

		runBenchmark(b, "packfile-nocache-memfs:"+data.name, func() packHandler[int64] {
			return newPackfileOpts(pack, idx,
				packfile.WithFs(memfs.New()))
		}, data.offsetHashMap)

		if runtime.GOOS != "windows" {
			runBenchmark(b, "mmap-pack-scanner:"+data.name, func() packHandler[uint64] {
				return newPackScanner(pack, idx, rev)
			}, data.offsetHashMap)
		}
	}
}

func runBenchmark[T int64OrUint64](b *testing.B, name string,
	newHandler func() packHandler[T], offsetHash map[uint64]string,
) {
	b.Helper()
	b.Run(name, func(b *testing.B) {
		for b.Loop() {
			p := newHandler()

			for o, h := range offsetHash {
				oid := plumbing.NewHash(h)

				obj, err := p.Get(oid)
				if err != nil {
					b.Fatal(err)
				}
				if obj.Hash().Compare(oid.Bytes()) != 0 {
					b.Error("hash mismatch")
				}

				obj2, err := p.GetByOffset(T(o))
				if err != nil {
					b.Fatal(err)
				}

				if oid.String() != h {
					b.Error("mismatch hash", h, oid.String())
				}

				if obj.Type() != obj2.Type() {
					b.Error("type mismatch")
				}
			}
		}
	})
}
