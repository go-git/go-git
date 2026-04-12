package compat

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustReadDir(t *testing.T, fs interface {
	ReadDir(string) ([]fs.DirEntry, error)
}, path string) []fs.DirEntry {
	t.Helper()
	entries, err := fs.ReadDir(path)
	require.NoError(t, err)
	return entries
}

func TestFileMapping(t *testing.T) {
	testHashMapping(t, func() HashMapping {
		fs := memfs.New()
		_ = fs.MkdirAll("objects", 0755)
		return NewFileMapping(fs, "objects")
	})
}

func TestFileMappingPersistence(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0755)

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Write a mapping with one instance.
	m1 := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	require.NoError(t, m1.Add(native, compat))

	// Open a fresh instance and verify it can read the persisted mapping.
	m2 := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	got, err := m2.NativeToCompat(native)
	require.NoError(t, err)
	assert.True(t, got.Equal(compat))

	entries, err := fs.ReadDir("objects/object-map")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0].Name(), "map-")
	assert.Contains(t, entries[0].Name(), ".map")
	_, err = fs.Stat("objects/loose-object-idx")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFileMappingPersistenceDefaultsToLegacyWrite(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	m := NewFileMapping(fs, "objects")
	require.NoError(t, m.Add(native, compat))

	data, err := readFile(fs, fs.Join("objects", "loose-object-idx"))
	require.NoError(t, err)
	assert.Contains(t, string(data), native.String()+" "+compat.String())

	_, err = fs.Stat(fs.Join("objects", "object-map"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFileMappingEmptyFile(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0755)

	m := NewFileMapping(fs, "objects")
	count, err := m.Count()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestFileMappingCountReturnsLoadError(t *testing.T) {
	fs := memfs.New()
	require.NoError(t, fs.MkdirAll("objects/object-map", 0o755))

	f, err := fs.OpenFile("objects/object-map/map-bad.map", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	_, err = f.Write([]byte("not-a-valid-map"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	m := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	count, err := m.Count()
	require.Error(t, err)
	assert.Zero(t, count)
}

func TestFileMappingReadsLegacyLooseObjectIdx(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0755)

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	f, err := fs.OpenFile("objects/loose-object-idx", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	_, err = f.Write([]byte("# loose-object-idx\n" + native.String() + " " + compat.String() + "\n"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	m := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	got, err := m.NativeToCompat(native)
	require.NoError(t, err)
	assert.Equal(t, compat, got)
}

func TestFileMappingReadsLegacyAndObjectMapTogether(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	native1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	native2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	compat2 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")

	legacy, err := fs.OpenFile("objects/loose-object-idx", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	_, err = legacy.Write([]byte(native1.String() + " " + compat1.String() + "\n"))
	require.NoError(t, err)
	require.NoError(t, legacy.Close())

	data, err := encodeMapEntries([]mapPair{{native: native2, compat: compat2}})
	require.NoError(t, err)
	mapPath, err := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap).mapPathForData(formatcfg.SHA1, data)
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(fs.Join("objects", "object-map"), 0o755))
	objectMapFile, err := fs.OpenFile(mapPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	_, err = objectMapFile.Write(data)
	require.NoError(t, err)
	require.NoError(t, objectMapFile.Close())

	m := NewFileMapping(fs, "objects")

	got1, err := m.NativeToCompat(native1)
	require.NoError(t, err)
	assert.Equal(t, compat1, got1)

	got2, err := m.NativeToCompat(native2)
	require.NoError(t, err)
	assert.Equal(t, compat2, got2)
}

func TestFileMappingCompact(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	m := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	native1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	native2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	compat2 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")

	require.NoError(t, m.Add(native1, compat1))
	require.NoError(t, m.Add(native2, compat2))
	require.NoError(t, m.Compact())

	entries, err := fs.ReadDir(fs.Join("objects", "object-map"))
	require.NoError(t, err)

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		assert.Contains(t, entry.Name(), "map-")
		assert.Contains(t, entry.Name(), ".map")
		count++
	}
	assert.Equal(t, 1, count)

	m2 := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	got1, err := m2.NativeToCompat(native1)
	require.NoError(t, err)
	assert.Equal(t, compat1, got1)

	got2, err := m2.NativeToCompat(native2)
	require.NoError(t, err)
	assert.Equal(t, compat2, got2)
}

func TestFileMappingObjectMapAddWritesIncrementalShards(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	m := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	native1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	native2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	compat2 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")

	require.NoError(t, m.Add(native1, compat1))
	entries := mustReadDir(t, fs, fs.Join("objects", "object-map"))
	require.Len(t, entries, 1)

	data1, err := readFile(fs, fs.Join("objects", "object-map", entries[0].Name()))
	require.NoError(t, err)
	nativeToCompat1, _, err := decodeMapFile(data1)
	require.NoError(t, err)
	require.Len(t, nativeToCompat1, 1)
	assert.Equal(t, compat1, nativeToCompat1[native1])

	require.NoError(t, m.Add(native2, compat2))
	entries = mustReadDir(t, fs, fs.Join("objects", "object-map"))
	require.Len(t, entries, 2)

	total := map[plumbing.Hash]plumbing.Hash{}
	for _, entry := range entries {
		data, err := readFile(fs, fs.Join("objects", "object-map", entry.Name()))
		require.NoError(t, err)
		nativeToCompat, _, err := decodeMapFile(data)
		require.NoError(t, err)
		require.Len(t, nativeToCompat, 1)
		for native, compat := range nativeToCompat {
			total[native] = compat
		}
	}

	assert.Equal(t, map[plumbing.Hash]plumbing.Hash{
		native1: compat1,
		native2: compat2,
	}, total)
}

func TestFileMappingObjectMapOverwriteUpdatesCurrentInstance(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	m := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	native1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	native2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	compat2 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")

	require.NoError(t, m.Add(native1, compat1))
	require.NoError(t, m.Add(native1, compat2))

	got, err := m.NativeToCompat(native1)
	require.NoError(t, err)
	assert.Equal(t, compat2, got)

	got, err = m.CompatToNative(compat2)
	require.NoError(t, err)
	assert.Equal(t, native1, got)

	_, err = m.CompatToNative(compat1)
	assert.ErrorIs(t, err, plumbing.ErrObjectNotFound)

	require.NoError(t, m.Add(native2, compat1))

	got, err = m.CompatToNative(compat1)
	require.NoError(t, err)
	assert.Equal(t, native2, got)
}

func TestFileMappingCompactLegacy(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	m := NewFileMapping(fs, "objects")
	native1 := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	native2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc")
	compat2 := plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")

	require.NoError(t, m.Add(native1, compat1))
	require.NoError(t, m.Add(native2, compat2))
	require.NoError(t, m.Compact())

	data, err := readFile(fs, fs.Join("objects", "loose-object-idx"))
	require.NoError(t, err)
	assert.Contains(t, string(data), native1.String()+" "+compat1.String())
	assert.Contains(t, string(data), native2.String()+" "+compat2.String())

	_, err = fs.Stat(fs.Join("objects", "object-map"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFileMappingCompactEmpty(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	m := NewFileMapping(fs, "objects")
	require.NoError(t, m.Compact())

	_, err := fs.Stat(fs.Join("objects", "loose-object-idx"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestDecodeMapFileErrors(t *testing.T) {
	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	valid, err := encodeMapEntries([]mapPair{{native: native, compat: compat}})
	require.NoError(t, err)

	t.Run("too small", func(t *testing.T) {
		_, _, err := decodeMapFile([]byte("small"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "map file too small")
	})

	t.Run("invalid signature", func(t *testing.T) {
		data := append([]byte(nil), valid...)
		copy(data[:4], []byte("BAD!"))
		_, _, err = decodeMapFile(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid signature")
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := append([]byte(nil), valid...)
		binary.BigEndian.PutUint32(data[4:8], 2)
		_, _, err = decodeMapFile(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported version")
	})

	t.Run("invalid trailer offset", func(t *testing.T) {
		data := append([]byte(nil), valid...)
		binary.BigEndian.PutUint64(data[52:60], uint64(len(data)+1))
		_, _, err := decodeMapFile(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid trailer offset")
	})

	t.Run("truncated format tables", func(t *testing.T) {
		data := append([]byte(nil), valid...)
		binary.BigEndian.PutUint32(data[12:16], 2)
		_, _, err := decodeMapFile(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "truncated format tables")
	})

	t.Run("invalid format offset", func(t *testing.T) {
		data := append([]byte(nil), valid...)
		binary.BigEndian.PutUint64(data[28:36], uint64(len(data)+1))
		_, _, err := decodeMapFile(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid format offset")
	})

	t.Run("invalid object ordering", func(t *testing.T) {
		data, err := encodeMapEntries([]mapPair{
			{native: native, compat: compat},
			{native: plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccc"), compat: plumbing.NewHash("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")},
		})
		require.NoError(t, err)
		offset := binary.BigEndian.Uint64(data[44:52])
		orderStart := int(offset) + (2 * formatcfg.SHA256.Size()) + (2 * formatcfg.SHA256.Size())
		binary.BigEndian.PutUint32(data[orderStart:orderStart+4], 99)
		_, _, err = decodeMapFile(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid object ordering")
	})
}

func TestHexFromBytes(t *testing.T) {
	assert.Equal(t, "00abff", hexFromBytes([]byte{0x00, 0xab, 0xff}))
}

func TestEncodeMapEntriesUsesShortAndFullTables(t *testing.T) {
	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	data, err := encodeMapEntries([]mapPair{{native: native, compat: compat}})
	require.NoError(t, err)

	nativeHashLen := formatcfg.SHA1.Size()
	nativeOffset := int(binary.BigEndian.Uint64(data[28:36]))
	shortTable := data[nativeOffset : nativeOffset+nativeHashLen]
	fullTable := data[nativeOffset+nativeHashLen : nativeOffset+(2*nativeHashLen)]
	assert.True(t, bytes.Equal(shortTable, fullTable))
}

func TestFileMappingConcurrentReadsAfterLoad(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	m := NewFileMapping(fs, "objects")
	require.NoError(t, m.Add(native, compat))

	_, err := m.NativeToCompat(native)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errCh := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				got, err := m.NativeToCompat(native)
				if err != nil {
					errCh <- err
					return
				}
				if got != compat {
					errCh <- fmt.Errorf("got compat hash %s, want %s", got, compat)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestFileMappingOverwriteLegacyPreservesExistingStateOnWriteFailure(t *testing.T) {
	base := memfs.New()
	_ = base.MkdirAll("objects", 0o755)

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	compat2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")

	m := NewFileMapping(base, "objects")
	require.NoError(t, m.Add(native, compat1))

	failing := failingRenameFS{Filesystem: base, failTarget: base.Join("objects", "loose-object-idx")}
	m = NewFileMapping(failing, "objects")
	err := m.Add(native, compat2)
	require.Error(t, err)

	reloaded := NewFileMapping(base, "objects")
	got, err := reloaded.NativeToCompat(native)
	require.NoError(t, err)
	assert.Equal(t, compat1, got)
}

func TestFileMappingOverwriteObjectMapPreservesExistingStateOnWriteFailure(t *testing.T) {
	base := memfs.New()
	_ = base.MkdirAll("objects", 0o755)

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	compat1 := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	compat2 := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")

	m := NewFileMappingWithWriteMode(base, "objects", FileMappingWriteObjectMap)
	require.NoError(t, m.Add(native, compat1))

	failing := failingRenameFS{Filesystem: base, failTargetPrefix: base.Join("objects", "object-map", mapSnapshotPrefix)}
	m = NewFileMappingWithWriteMode(failing, "objects", FileMappingWriteObjectMap)
	err := m.Add(native, compat2)
	require.Error(t, err)

	reloaded := NewFileMappingWithWriteMode(base, "objects", FileMappingWriteObjectMap)
	got, err := reloaded.NativeToCompat(native)
	require.NoError(t, err)
	assert.Equal(t, compat1, got)
}

func TestFileMappingSnapshotFileWinsDuringRecovery(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("objects", 0o755)

	native := plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	oldCompat := plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	newCompat := plumbing.NewHash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")

	oldData, err := encodeMapEntries([]mapPair{{native: native, compat: oldCompat}})
	require.NoError(t, err)
	oldPath, err := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap).mapPathForData(formatcfg.SHA1, oldData)
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(fs.Join("objects", "object-map"), 0o755))
	f, err := fs.OpenFile(oldPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	_, err = f.Write(oldData)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	newData, err := encodeMapEntries([]mapPair{{native: native, compat: newCompat}})
	require.NoError(t, err)
	snapshotPath := fs.Join("objects", "object-map", mapSnapshotPrefix+hexFromBytes(mustChecksumForTest(t, formatcfg.SHA1, newData))+mapFileExt)
	f, err = fs.OpenFile(snapshotPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	_, err = f.Write(newData)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	m := NewFileMappingWithWriteMode(fs, "objects", FileMappingWriteObjectMap)
	got, err := m.NativeToCompat(native)
	require.NoError(t, err)
	assert.Equal(t, newCompat, got)
}

func mustChecksumForTest(t *testing.T, of formatcfg.ObjectFormat, data []byte) []byte {
	t.Helper()
	sum, err := checksumForFormat(of, data)
	require.NoError(t, err)
	return sum
}

type failingRenameFS struct {
	billy.Filesystem
	failTarget       string
	failTargetPrefix string
}

func (fs failingRenameFS) Rename(oldpath, newpath string) error {
	if fs.failTarget != "" && newpath == fs.failTarget {
		return fmt.Errorf("forced rename failure for %s", newpath)
	}
	if fs.failTargetPrefix != "" && strings.HasPrefix(newpath, fs.failTargetPrefix) {
		return fmt.Errorf("forced rename failure for %s", newpath)
	}
	return fs.Filesystem.Rename(oldpath, newpath)
}
