package oidmap

import (
	"bytes"
	stdbinary "encoding/binary"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	gitbinary "github.com/go-git/go-git/v6/utils/binary"
)

const (
	objectMapDirName  = "object-map"
	mapFilePrefix     = "map-"
	mapSnapshotPrefix = "map-zsnapshot-"
	mapFileExt        = ".map"
	mapSignature      = "LMAP"
	mapVersion        = uint32(1)
	mapHeaderSize     = uint32(60)
)

func (m *File) writeObjectMap(pairs []mapPair) error {
	data, err := encodeMapEntries(pairs)
	if err != nil {
		return err
	}
	nativeFmt, err := objectFormatFromHash(pairs[0].native)
	if err != nil {
		return err
	}
	mapPath, err := m.mapPathForData(nativeFmt, data)
	if err != nil {
		return err
	}

	if err := m.fs.MkdirAll(m.mapDir(), 0o755); err != nil {
		return fmt.Errorf("create object-map dir: %w", err)
	}
	if err := removeLegacyTextIndex(m.fs, m.legacyIdxPath()); err != nil {
		return err
	}

	return atomicWriteFile(m.fs, mapPath, data, 0o644)
}

func (m *File) replaceObjectMapSnapshot(pairs []mapPair) error {
	data, err := encodeMapEntries(pairs)
	if err != nil {
		return err
	}
	nativeFmt, err := objectFormatFromHash(pairs[0].native)
	if err != nil {
		return err
	}
	checksum, err := checksumForFormat(nativeFmt, data)
	if err != nil {
		return err
	}
	snapshotPath := m.fs.Join(m.mapDir(), mapSnapshotPrefix+hexFromBytes(checksum)+mapFileExt)
	finalPath := m.fs.Join(m.mapDir(), mapFilePrefix+hexFromBytes(checksum)+mapFileExt)

	if err := m.fs.MkdirAll(m.mapDir(), 0o755); err != nil {
		return fmt.Errorf("create object-map dir: %w", err)
	}
	if err := removeLegacyTextIndex(m.fs, m.legacyIdxPath()); err != nil {
		return err
	}
	if err := atomicWriteFile(m.fs, snapshotPath, data, 0o644); err != nil {
		return err
	}
	if err := removeExistingMapFilesExcept(m.fs, m.mapDir(), snapshotPath); err != nil {
		return err
	}
	if err := m.fs.Rename(snapshotPath, finalPath); err != nil {
		return fmt.Errorf("rename object-map snapshot: %w", err)
	}
	return nil
}

func encodeMapEntries(pairs []mapPair) ([]byte, error) {
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no map entries")
	}

	nativeFmt, err := objectFormatFromHash(pairs[0].native)
	if err != nil {
		return nil, err
	}
	compatFmt, err := objectFormatFromHash(pairs[0].compat)
	if err != nil {
		return nil, err
	}
	for _, pair := range pairs[1:] {
		of, err := objectFormatFromHash(pair.native)
		if err != nil {
			return nil, err
		}
		cf, err := objectFormatFromHash(pair.compat)
		if err != nil {
			return nil, err
		}
		if of != nativeFmt || cf != compatFmt {
			return nil, fmt.Errorf("mixed object formats in map entries")
		}
	}

	const numFormats = uint32(2)

	numObjects := uint32(len(pairs))
	nativeHashLen := nativeFmt.Size()
	compatHashLen := compatFmt.Size()
	nativeOffset := uint64(mapHeaderSize)
	compatOffset, err := gitbinary.Align(nativeOffset+uint64(int(numObjects)*(nativeHashLen+nativeHashLen+4)), 4)
	if err != nil {
		return nil, err
	}
	trailerOffset, err := gitbinary.Align(compatOffset+uint64(int(numObjects)*(compatHashLen+compatHashLen+4)), 4)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString(mapSignature)
	if err := gitbinary.Write(&buf, mapVersion, mapHeaderSize, numObjects, numFormats); err != nil {
		return nil, err
	}

	buf.Write(formatID(nativeFmt))
	if err := gitbinary.Write(&buf, uint32(nativeHashLen), nativeOffset); err != nil {
		return nil, err
	}

	buf.Write(formatID(compatFmt))
	if err := gitbinary.Write(&buf, uint32(compatHashLen), compatOffset); err != nil {
		return nil, err
	}

	if err := gitbinary.WriteUint64(&buf, trailerOffset); err != nil {
		return nil, err
	}

	// The LMAP container stores a shortened-name table and a full-name table
	// for each object format. We currently use full-width names in both.
	for _, pair := range pairs {
		buf.Write(pair.native.Bytes()[:nativeHashLen])
	}
	for _, pair := range pairs {
		buf.Write(pair.native.Bytes()[:nativeHashLen])
	}
	for range pairs {
		if err := gitbinary.WriteUint32(&buf, 1); err != nil {
			return nil, err
		}
	}
	if err := gitbinary.WritePadding(&buf, buf.Len(), 4); err != nil {
		return nil, err
	}

	for _, pair := range pairs {
		buf.Write(pair.compat.Bytes()[:compatHashLen])
	}
	for _, pair := range pairs {
		buf.Write(pair.compat.Bytes()[:compatHashLen])
	}
	for i := range pairs {
		if err := gitbinary.WriteUint32(&buf, uint32(i)); err != nil {
			return nil, err
		}
	}
	if err := gitbinary.WritePadding(&buf, buf.Len(), 4); err != nil {
		return nil, err
	}

	checksum, err := checksumForFormat(nativeFmt, buf.Bytes())
	if err != nil {
		return nil, err
	}
	buf.Write(checksum)

	return buf.Bytes(), nil
}

func decodeMapFile(data []byte) (map[plumbing.Hash]plumbing.Hash, map[plumbing.Hash]plumbing.Hash, error) {
	if len(data) < 60 {
		return nil, nil, fmt.Errorf("map file too small")
	}
	if string(data[:4]) != mapSignature {
		return nil, nil, fmt.Errorf("invalid signature")
	}
	if stdbinary.BigEndian.Uint32(data[4:8]) != mapVersion {
		return nil, nil, fmt.Errorf("unsupported version")
	}

	headerLen := stdbinary.BigEndian.Uint32(data[8:12])
	numObjects := stdbinary.BigEndian.Uint32(data[12:16])
	numFormats := stdbinary.BigEndian.Uint32(data[16:20])
	if numObjects == 0 {
		return nil, nil, fmt.Errorf("unsupported map shape")
	}
	if numFormats != 2 {
		return nil, nil, fmt.Errorf("unsupported number of formats: %d", numFormats)
	}
	if uint64(headerLen) > uint64(len(data)) {
		return nil, nil, fmt.Errorf("invalid header length")
	}

	type formatInfo struct {
		format   formatcfg.ObjectFormat
		shortLen uint32
		offset   uint64
		hashLen  int
	}
	var formats []formatInfo
	pos := 20
	for range numFormats {
		if pos > len(data)-16 {
			return nil, nil, fmt.Errorf("truncated format descriptors")
		}
		id := string(data[pos : pos+4])
		shortLen := stdbinary.BigEndian.Uint32(data[pos+4 : pos+8])
		offset := stdbinary.BigEndian.Uint64(data[pos+8 : pos+16])
		of, err := objectFormatFromID(id)
		if err != nil {
			return nil, nil, err
		}
		formats = append(formats, formatInfo{
			format:   of,
			shortLen: shortLen,
			offset:   offset,
			hashLen:  of.Size(),
		})
		pos += 16
	}

	if pos > len(data)-8 {
		return nil, nil, fmt.Errorf("truncated trailer offset")
	}
	if uint64(headerLen) < uint64(pos+8) {
		return nil, nil, fmt.Errorf("invalid header length")
	}
	trailerOffset := stdbinary.BigEndian.Uint64(data[pos : pos+8])
	if trailerOffset >= uint64(len(data)) {
		return nil, nil, fmt.Errorf("invalid trailer offset")
	}
	checksumLen := formats[0].hashLen
	if uint64(checksumLen) > uint64(len(data)) || trailerOffset > uint64(len(data))-uint64(checksumLen) {
		return nil, nil, fmt.Errorf("invalid trailer offset")
	}
	checksumStart := int(trailerOffset)
	checksumEnd := checksumStart + checksumLen
	if checksumEnd != len(data) {
		return nil, nil, fmt.Errorf("invalid trailer offset")
	}

	fulls := make([][]plumbing.Hash, len(formats))
	orderings := make([][]uint32, len(formats))
	for i, info := range formats {
		if info.offset < uint64(headerLen) || info.offset >= uint64(len(data)) {
			return nil, nil, fmt.Errorf("invalid format offset")
		}

		shortTableLen := uint64(info.shortLen) * uint64(numObjects)
		fullTableLen := uint64(info.hashLen) * uint64(numObjects)
		orderTableLen := uint64(0)
		if i > 0 {
			orderTableLen = 4 * uint64(numObjects)
		}
		end := info.offset
		for _, tableLen := range []uint64{shortTableLen, fullTableLen, orderTableLen} {
			if tableLen > trailerOffset || end > trailerOffset-tableLen {
				return nil, nil, fmt.Errorf("truncated format tables")
			}
			end += tableLen
		}

		start := int(info.offset)
		shortTableLenInt := int(shortTableLen)
		fullTableLenInt := int(fullTableLen)
		hashLen := info.hashLen
		shortLen := int(info.shortLen)
		if shortLen == 0 {
			return nil, nil, fmt.Errorf("truncated format tables")
		}

		for j := range numObjects {
			offset := start + int(j)*shortLen
			_, ok := plumbing.FromHex(hexFromBytes(data[offset : offset+shortLen]))
			if !ok {
				return nil, nil, fmt.Errorf("invalid shortened hash")
			}
		}

		fullStart := start + shortTableLenInt
		var names []plumbing.Hash
		for j := range numObjects {
			offset := fullStart + int(j)*hashLen
			h, ok := plumbing.FromBytes(data[offset : offset+hashLen])
			if !ok {
				return nil, nil, fmt.Errorf("invalid full hash")
			}
			names = append(names, h)
		}
		fulls[i] = names

		if i > 0 {
			orderStart := fullStart + fullTableLenInt
			order := make([]uint32, 0, numObjects)
			for j := range numObjects {
				offset := orderStart + int(j)*4
				order = append(order, stdbinary.BigEndian.Uint32(data[offset:offset+4]))
			}
			orderings[i] = order
		}
	}

	nativeToCompat := make(map[plumbing.Hash]plumbing.Hash, numObjects)
	compatToNative := make(map[plumbing.Hash]plumbing.Hash, numObjects)
	first := fulls[0]
	for i := 1; i < len(formats); i++ {
		for compatIdx, firstIdx := range orderings[i] {
			if int(firstIdx) >= len(first) || compatIdx >= len(fulls[i]) {
				return nil, nil, fmt.Errorf("invalid object ordering")
			}
			native := first[firstIdx]
			compat := fulls[i][compatIdx]
			setMapping(nativeToCompat, compatToNative, native, compat)
		}
	}

	checksum, err := checksumForFormat(formats[0].format, data[:checksumStart])
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(data[checksumStart:checksumEnd], checksum) {
		return nil, nil, fmt.Errorf("checksum mismatch")
	}

	return nativeToCompat, compatToNative, nil
}

func removeExistingMapFiles(fs billy.Filesystem, dir string) error {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read object-map dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !isMapFile(entry.Name()) {
			continue
		}
		if err := fs.Remove(fs.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("remove map file %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func removeExistingMapFilesExcept(fs billy.Filesystem, dir, keepPath string) error {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read object-map dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !isMapFile(entry.Name()) {
			continue
		}
		path := fs.Join(dir, entry.Name())
		if path == keepPath {
			continue
		}
		if err := fs.Remove(path); err != nil {
			return fmt.Errorf("remove map file %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func isMapFile(name string) bool {
	return strings.HasPrefix(name, mapFilePrefix) && strings.HasSuffix(name, mapFileExt)
}

func isSnapshotMapFile(name string) bool {
	return strings.HasPrefix(name, mapSnapshotPrefix) && strings.HasSuffix(name, mapFileExt)
}

func sortMapEntries(entries []os.DirEntry) {
	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		aSnapshot := isSnapshotMapFile(a.Name())
		bSnapshot := isSnapshotMapFile(b.Name())
		if aSnapshot != bSnapshot {
			if aSnapshot {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Name(), b.Name())
	})
}
