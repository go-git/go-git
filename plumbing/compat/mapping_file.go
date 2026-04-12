package compat

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
)

const (
	objectMapDirName         = "object-map"
	legacyLooseObjectIdxFile = "loose-object-idx"
	mapFilePrefix            = "map-"
	mapSnapshotPrefix        = "map-zsnapshot-"
	mapFileExt               = ".map"
	mapSignature             = "LMAP"
	mapVersion               = uint32(1)
	mapHeaderSize            = uint32(60)
)

// FileMapping is a filesystem-backed implementation of HashMapping.
// It always reads both the legacy `objects/loose-object-idx` format and the
// newer `objects/object-map/*.map` files, and writes using the configured mode.
type FileMapping struct {
	mu   sync.RWMutex
	fs   billy.Filesystem
	path string // directory containing the objects directory
	mode FileMappingWriteMode

	loaded         bool
	nativeToCompat map[plumbing.Hash]plumbing.Hash
	compatToNative map[plumbing.Hash]plumbing.Hash
}

// FileMappingWriteMode controls how compat mappings are persisted on disk.
type FileMappingWriteMode uint8

const (
	// FileMappingWriteLegacy writes mappings to objects/loose-object-idx.
	FileMappingWriteLegacy FileMappingWriteMode = iota
	// FileMappingWriteObjectMap writes mappings to objects/object-map/map-*.map.
	FileMappingWriteObjectMap
)

// NewFileMapping creates a FileMapping backed by the given filesystem and
// directory path (typically the objects directory, e.g. ".git/objects").
func NewFileMapping(fs billy.Filesystem, path string) *FileMapping {
	return NewFileMappingWithWriteMode(fs, path, FileMappingWriteLegacy)
}

// NewFileMappingWithWriteMode creates a FileMapping with an explicit on-disk
// write mode. Reading always supports both legacy and object-map formats.
func NewFileMappingWithWriteMode(fs billy.Filesystem, path string, mode FileMappingWriteMode) *FileMapping {
	return &FileMapping{
		fs:             fs,
		path:           path,
		mode:           mode,
		nativeToCompat: make(map[plumbing.Hash]plumbing.Hash),
		compatToNative: make(map[plumbing.Hash]plumbing.Hash),
	}
}

func (m *FileMapping) mapDir() string {
	return m.fs.Join(m.path, objectMapDirName)
}

func (m *FileMapping) mapPathForData(nativeFmt formatcfg.ObjectFormat, data []byte) (string, error) {
	checksum, err := checksumForFormat(nativeFmt, data)
	if err != nil {
		return "", err
	}
	return m.fs.Join(m.mapDir(), mapFilePrefix+hexFromBytes(checksum)+mapFileExt), nil
}

func (m *FileMapping) legacyIdxPath() string {
	return m.fs.Join(m.path, legacyLooseObjectIdxFile)
}

// load reads all map files into memory. Must be called with m.mu held
// (at least for writing).
func (m *FileMapping) load() error {
	if m.loaded {
		return nil
	}

	entries, err := m.fs.ReadDir(m.mapDir())
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read object-map dir: %w", err)
		}
		entries = nil
	}
	sortMapEntries(entries)

	for _, entry := range entries {
		if entry.IsDir() || !isMapFile(entry.Name()) {
			continue
		}

		path := m.fs.Join(m.mapDir(), entry.Name())
		data, err := readFile(m.fs, path)
		if err != nil {
			return fmt.Errorf("read map file %s: %w", entry.Name(), err)
		}

		nativeToCompat, compatToNative, err := decodeMapFile(data)
		if err != nil {
			return fmt.Errorf("decode map file %s: %w", entry.Name(), err)
		}

		for native, compat := range nativeToCompat {
			m.nativeToCompat[native] = compat
		}
		for compat, native := range compatToNative {
			m.compatToNative[compat] = native
		}
	}

	if err := m.loadLegacyTextIndex(); err != nil {
		return err
	}

	m.loaded = true
	return nil
}

func (m *FileMapping) NativeToCompat(native plumbing.Hash) (plumbing.Hash, error) {
	m.mu.RLock()
	if m.loaded {
		h, ok := m.nativeToCompat[native]
		m.mu.RUnlock()
		if !ok {
			return plumbing.Hash{}, plumbing.ErrObjectNotFound
		}
		return h, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return plumbing.Hash{}, err
	}
	h, ok := m.nativeToCompat[native]
	if !ok {
		return plumbing.Hash{}, plumbing.ErrObjectNotFound
	}
	return h, nil
}

func (m *FileMapping) CompatToNative(compat plumbing.Hash) (plumbing.Hash, error) {
	m.mu.RLock()
	if m.loaded {
		h, ok := m.compatToNative[compat]
		m.mu.RUnlock()
		if !ok {
			return plumbing.Hash{}, plumbing.ErrObjectNotFound
		}
		return h, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return plumbing.Hash{}, err
	}
	h, ok := m.compatToNative[compat]
	if !ok {
		return plumbing.Hash{}, plumbing.ErrObjectNotFound
	}
	return h, nil
}

func (m *FileMapping) Add(native, compat plumbing.Hash) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.load(); err != nil {
		return err
	}

	if existing, ok := m.nativeToCompat[native]; ok {
		if existing.Equal(compat) {
			return nil
		}
		return m.overwriteMapping(native, compat)
	}
	if existing, ok := m.compatToNative[compat]; ok {
		if existing.Equal(native) {
			return nil
		}
		return m.overwriteMapping(native, compat)
	}

	switch m.mode {
	case FileMappingWriteLegacy:
		if err := m.writeMappings(append(sortedPairs(m.nativeToCompat), mapPair{native: native, compat: compat})); err != nil {
			return err
		}
	case FileMappingWriteObjectMap:
		if err := m.writeObjectMap([]mapPair{{native: native, compat: compat}}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported compat mapping write mode: %d", m.mode)
	}

	m.nativeToCompat[native] = compat
	m.compatToNative[compat] = native
	return nil
}

func (m *FileMapping) overwriteMapping(native, compat plumbing.Hash) error {
	pairs := make([]mapPair, 0, len(m.nativeToCompat)+1)
	for existingNative, existingCompat := range m.nativeToCompat {
		if existingNative.Equal(native) || existingCompat.Equal(compat) {
			continue
		}
		pairs = append(pairs, mapPair{native: existingNative, compat: existingCompat})
	}
	pairs = append(pairs, mapPair{native: native, compat: compat})
	slices.SortFunc(pairs, func(a, b mapPair) int {
		return bytes.Compare(a.native.Bytes(), b.native.Bytes())
	})

	if m.mode == FileMappingWriteObjectMap {
		if err := m.replaceObjectMapSnapshot(pairs); err != nil {
			return err
		}
		m.nativeToCompat = make(map[plumbing.Hash]plumbing.Hash, len(pairs))
		m.compatToNative = make(map[plumbing.Hash]plumbing.Hash, len(pairs))
		for _, pair := range pairs {
			m.nativeToCompat[pair.native] = pair.compat
			m.compatToNative[pair.compat] = pair.native
		}
		return nil
	}
	if err := m.writeMappings(pairs); err != nil {
		return err
	}

	for existingNative, existingCompat := range m.nativeToCompat {
		if existingNative.Equal(native) || existingCompat.Equal(compat) {
			delete(m.nativeToCompat, existingNative)
			delete(m.compatToNative, existingCompat)
		}
	}
	m.nativeToCompat[native] = compat
	m.compatToNative[compat] = native
	return nil
}

func (m *FileMapping) Count() (int, error) {
	m.mu.RLock()
	if m.loaded {
		count := len(m.nativeToCompat)
		m.mu.RUnlock()
		return count, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	if err := m.load(); err != nil {
		m.mu.Unlock()
		return 0, err
	}
	count := len(m.nativeToCompat)
	m.mu.Unlock()

	return count, nil
}

// Compact rewrites all currently known mappings using the active write mode.
func (m *FileMapping) Compact() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.load(); err != nil {
		return err
	}
	if len(m.nativeToCompat) == 0 {
		return nil
	}

	if m.mode == FileMappingWriteObjectMap {
		return m.replaceObjectMapSnapshot(sortedPairs(m.nativeToCompat))
	}

	return m.writeMappings(sortedPairs(m.nativeToCompat))
}

func (m *FileMapping) writeMappings(pairs []mapPair) error {
	if len(pairs) == 0 {
		return nil
	}

	switch m.mode {
	case FileMappingWriteLegacy:
		return m.writeLegacyTextIndex(pairs)
	case FileMappingWriteObjectMap:
		return m.writeObjectMap(pairs)
	default:
		return fmt.Errorf("unsupported compat mapping write mode: %d", m.mode)
	}
}

func (m *FileMapping) writeLegacyTextIndex(pairs []mapPair) error {
	if err := removeExistingMapFiles(m.fs, m.mapDir()); err != nil {
		return err
	}

	var buf bytes.Buffer
	for _, pair := range pairs {
		if _, err := fmt.Fprintf(&buf, "%s %s\n", pair.native, pair.compat); err != nil {
			return fmt.Errorf("write legacy loose-object-idx: %w", err)
		}
	}

	return atomicWriteFile(m.fs, m.path, m.legacyIdxPath(), buf.Bytes(), 0o644)
}

func (m *FileMapping) writeObjectMap(pairs []mapPair) error {
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

	return atomicWriteFile(m.fs, m.mapDir(), mapPath, data, 0o644)
}

func (m *FileMapping) replaceObjectMapSnapshot(pairs []mapPair) error {
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
	if err := atomicWriteFile(m.fs, m.mapDir(), snapshotPath, data, 0o644); err != nil {
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

type mapPair struct {
	native plumbing.Hash
	compat plumbing.Hash
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

	const (
		numFormats = uint32(2)
	)
	numObjects := uint32(len(pairs))

	nativeHashLen := nativeFmt.Size()
	compatHashLen := compatFmt.Size()
	nativeOffset := uint64(mapHeaderSize)
	compatOffset := align4(nativeOffset + uint64(int(numObjects)*(nativeHashLen+nativeHashLen+4)))
	trailerOffset := align4(compatOffset + uint64(int(numObjects)*(compatHashLen+compatHashLen+4)))

	var buf bytes.Buffer
	buf.WriteString(mapSignature)
	writeUint32(&buf, mapVersion)
	writeUint32(&buf, mapHeaderSize)
	writeUint32(&buf, numObjects)
	writeUint32(&buf, numFormats)

	buf.Write(formatID(nativeFmt))
	writeUint32(&buf, uint32(nativeHashLen))
	writeUint64(&buf, nativeOffset)

	buf.Write(formatID(compatFmt))
	writeUint32(&buf, uint32(compatHashLen))
	writeUint64(&buf, compatOffset)

	writeUint64(&buf, trailerOffset)

	// The LMAP container stores a shortened-name table and a full-name table
	// for each object format. We currently use full-width names in both.
	for _, pair := range pairs {
		buf.Write(pair.native.Bytes()[:nativeHashLen])
	}
	for _, pair := range pairs {
		buf.Write(pair.native.Bytes()[:nativeHashLen])
	}
	for range pairs {
		writeUint32(&buf, 1)
	}
	writePadding(&buf)

	// The compat format uses the same shortened/full table layout.
	for _, pair := range pairs {
		buf.Write(pair.compat.Bytes()[:compatHashLen])
	}
	for _, pair := range pairs {
		buf.Write(pair.compat.Bytes()[:compatHashLen])
	}
	for i := range pairs {
		writeUint32(&buf, uint32(i))
	}
	writePadding(&buf)

	checksum, err := checksumForFormat(nativeFmt, buf.Bytes())
	if err != nil {
		return nil, err
	}
	buf.Write(checksum)

	return buf.Bytes(), nil
}

func (m *FileMapping) loadLegacyTextIndex() error {
	data, err := readFile(m.fs, m.legacyIdxPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read legacy loose-object-idx: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		native, nok := plumbing.FromHex(parts[0])
		compat, cok := plumbing.FromHex(parts[1])
		if !nok || !cok {
			continue
		}
		m.nativeToCompat[native] = compat
		m.compatToNative[compat] = native
	}
	return nil
}

func decodeMapFile(data []byte) (map[plumbing.Hash]plumbing.Hash, map[plumbing.Hash]plumbing.Hash, error) {
	if len(data) < 60 {
		return nil, nil, fmt.Errorf("map file too small")
	}
	if string(data[:4]) != mapSignature {
		return nil, nil, fmt.Errorf("invalid signature")
	}
	if binary.BigEndian.Uint32(data[4:8]) != mapVersion {
		return nil, nil, fmt.Errorf("unsupported version")
	}

	headerLen := binary.BigEndian.Uint32(data[8:12])
	numObjects := binary.BigEndian.Uint32(data[12:16])
	numFormats := binary.BigEndian.Uint32(data[16:20])
	if numObjects == 0 || numFormats < 2 {
		return nil, nil, fmt.Errorf("unsupported map shape")
	}

	type formatInfo struct {
		format   formatcfg.ObjectFormat
		shortLen uint32
		offset   uint64
		hashLen  int
	}
	formats := make([]formatInfo, 0, numFormats)
	pos := 20
	for i := uint32(0); i < numFormats; i++ {
		id := string(data[pos : pos+4])
		shortLen := binary.BigEndian.Uint32(data[pos+4 : pos+8])
		offset := binary.BigEndian.Uint64(data[pos+8 : pos+16])
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

	trailerOffset := binary.BigEndian.Uint64(data[pos : pos+8])
	if trailerOffset >= uint64(len(data)) {
		return nil, nil, fmt.Errorf("invalid trailer offset")
	}

	fulls := make([][]plumbing.Hash, len(formats))
	orderings := make([][]uint32, len(formats))
	for i, info := range formats {
		start := int(info.offset)
		if start < int(headerLen) || start >= len(data) {
			return nil, nil, fmt.Errorf("invalid format offset")
		}

		shortTableLen := int(info.shortLen) * int(numObjects)
		fullTableLen := info.hashLen * int(numObjects)
		orderTableLen := 0
		if i > 0 {
			orderTableLen = 4 * int(numObjects)
		}
		end := start + shortTableLen + fullTableLen + orderTableLen
		if end > len(data) {
			return nil, nil, fmt.Errorf("truncated format tables")
		}

		for j := uint32(0); j < numObjects; j++ {
			offset := start + int(j)*int(info.shortLen)
			_, ok := plumbing.FromHex(hexFromBytes(data[offset : offset+int(info.shortLen)]))
			if !ok {
				return nil, nil, fmt.Errorf("invalid shortened hash")
			}
		}

		fullStart := start + shortTableLen
		var names []plumbing.Hash
		for j := uint32(0); j < numObjects; j++ {
			offset := fullStart + int(j)*info.hashLen
			h, ok := plumbing.FromBytes(data[offset : offset+info.hashLen])
			if !ok {
				return nil, nil, fmt.Errorf("invalid full hash")
			}
			names = append(names, h)
		}
		fulls[i] = names

		if i > 0 {
			orderStart := fullStart + fullTableLen
			order := make([]uint32, 0, numObjects)
			for j := uint32(0); j < numObjects; j++ {
				offset := orderStart + int(j)*4
				order = append(order, binary.BigEndian.Uint32(data[offset:offset+4]))
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
			nativeToCompat[native] = compat
			compatToNative[compat] = native
		}
	}

	return nativeToCompat, compatToNative, nil
}

func readFile(fs billy.Filesystem, path string) ([]byte, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

func removeLegacyTextIndex(fs billy.Filesystem, path string) error {
	if err := fs.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy loose-object-idx: %w", err)
	}
	return nil
}

func sortedPairs(m map[plumbing.Hash]plumbing.Hash) []mapPair {
	pairs := make([]mapPair, 0, len(m))
	for native, compat := range m {
		pairs = append(pairs, mapPair{native: native, compat: compat})
	}
	slices.SortFunc(pairs, func(a, b mapPair) int {
		return bytes.Compare(a.native.Bytes(), b.native.Bytes())
	})
	return pairs
}

func objectFormatFromHash(h plumbing.Hash) (formatcfg.ObjectFormat, error) {
	switch h.Size() {
	case formatcfg.SHA1.Size():
		return formatcfg.SHA1, nil
	case formatcfg.SHA256.Size():
		return formatcfg.SHA256, nil
	default:
		return "", fmt.Errorf("unsupported hash length")
	}
}

func objectFormatFromID(id string) (formatcfg.ObjectFormat, error) {
	switch id {
	case "sha1":
		return formatcfg.SHA1, nil
	case "s256":
		return formatcfg.SHA256, nil
	case "sha256":
		return formatcfg.SHA256, nil
	default:
		return "", fmt.Errorf("unsupported format id %q", id)
	}
}

func formatID(of formatcfg.ObjectFormat) []byte {
	switch of {
	case formatcfg.SHA1:
		return []byte("sha1")
	case formatcfg.SHA256:
		return []byte("s256")
	default:
		return []byte("unkn")
	}
}

func checksumForFormat(of formatcfg.ObjectFormat, data []byte) ([]byte, error) {
	switch of {
	case formatcfg.SHA1:
		sum := sha1.Sum(data)
		return sum[:], nil
	case formatcfg.SHA256:
		sum := sha256.Sum256(data)
		return sum[:], nil
	default:
		return nil, fmt.Errorf("unsupported checksum format %q", of)
	}
}

func writeUint32(buf *bytes.Buffer, v uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], v)
	buf.Write(tmp[:])
}

func writeUint64(buf *bytes.Buffer, v uint64) {
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], v)
	buf.Write(tmp[:])
}

func writePadding(buf *bytes.Buffer) {
	for buf.Len()%4 != 0 {
		buf.WriteByte(0)
	}
}

func align4(v uint64) uint64 {
	if rem := v % 4; rem != 0 {
		return v + (4 - rem)
	}
	return v
}

func hexFromBytes(b []byte) string {
	return hex.EncodeToString(b)
}

func atomicWriteFile(fs billy.Filesystem, tempDir, target string, data []byte, perm os.FileMode) (err error) {
	f, err := fs.TempFile(tempDir, ".tmp-compat-map-")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", target, err)
	}
	tempPath := f.Name()
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = fs.Remove(tempPath)
		}
	}()

	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("write temp file for %s: %w", target, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", target, err)
	}
	if chmodFS, ok := fs.(billy.Chmod); ok {
		if err = chmodFS.Chmod(tempPath, perm); err != nil {
			return fmt.Errorf("chmod temp file for %s: %w", target, err)
		}
	}
	if err = fs.Rename(tempPath, target); err != nil {
		return fmt.Errorf("rename temp file for %s: %w", target, err)
	}
	return nil
}
