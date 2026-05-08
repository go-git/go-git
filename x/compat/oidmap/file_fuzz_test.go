package oidmap

import (
	"os"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"

	"github.com/go-git/go-git/v6/plumbing"
)

func FuzzFileFormatDecoders(f *testing.F) {
	nativeSHA1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compatSHA256 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	otherCompatSHA256 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	nativeSHA256 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	compatSHA1 := plumbing.NewHash("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")

	objectMapSHA1ToSHA256 := mustEncodeMapEntriesForFuzz(f, []mapPair{{native: nativeSHA1, compat: compatSHA256}})
	objectMapSHA256ToSHA1 := mustEncodeMapEntriesForFuzz(f, []mapPair{{native: nativeSHA256, compat: compatSHA1}})

	f.Add(objectMapSHA1ToSHA256, []byte(nativeSHA1.String()+" "+otherCompatSHA256.String()+"\n"))
	f.Add(objectMapSHA1ToSHA256, []byte{})
	f.Add(objectMapSHA256ToSHA1, []byte(nativeSHA256.String()+" "+compatSHA1.String()+"\n"))
	f.Add([]byte("small"), []byte("# comment\ninvalid\n"))
	f.Add(make([]byte, mapHeaderSize), []byte(nativeSHA1.String()+" "+compatSHA256.String()+"\n"))

	f.Fuzz(func(t *testing.T, objectMapData, legacyData []byte) {
		if len(objectMapData)+len(legacyData) > 1<<20 {
			t.Skip()
		}

		legacy := NewFile(memfs.New(), "objects")
		if err := legacy.fs.MkdirAll("objects", 0o755); err != nil {
			t.Fatalf("create legacy objects dir: %v", err)
		}
		writeFuzzFile(t, legacy.fs, legacy.legacyIdxPath(), legacyData)
		_ = legacy.loadLegacyTextIndex()

		fs := memfs.New()
		if err := fs.MkdirAll("objects/object-map", 0o755); err != nil {
			t.Fatalf("create object-map dir: %v", err)
		}

		writeFuzzFile(t, fs, "objects/object-map/map-fuzz.map", objectMapData)
		writeFuzzFile(t, fs, "objects/loose-object-idx", legacyData)

		file := NewFile(fs, "objects")
		_ = file.load()
	})
}

func mustEncodeMapEntriesForFuzz(f *testing.F, pairs []mapPair) []byte {
	f.Helper()

	data, err := encodeMapEntries(pairs)
	if err != nil {
		f.Fatalf("encode map entries: %v", err)
	}
	return data
}

func writeFuzzFile(t *testing.T, fs billy.Filesystem, path string, data []byte) {
	t.Helper()

	f, err := fs.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("open fuzz file %s: %v", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		t.Fatalf("write fuzz file %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close fuzz file %s: %v", path, err)
	}
}
