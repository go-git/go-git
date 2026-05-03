package oidmap

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"sync"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
)

// File is a filesystem-backed implementation of Map.
// It always reads both the legacy `objects/loose-object-idx` format and the
// newer `objects/object-map/*.map` files, and writes using the configured mode.
type File struct {
	mu   sync.RWMutex
	fs   billy.Filesystem
	path string // directory containing the objects directory
	mode FileWriteMode

	loaded         bool
	nativeToCompat map[plumbing.Hash]plumbing.Hash
	compatToNative map[plumbing.Hash]plumbing.Hash
}

// FileWriteMode controls how compat mappings are persisted on disk.
type FileWriteMode uint8

const (
	// FileWriteLegacy writes mappings to objects/loose-object-idx.
	FileWriteLegacy FileWriteMode = iota
	// FileWriteObjectMap writes mappings to objects/object-map/map-*.map.
	FileWriteObjectMap
)

// NewFile creates a File backed by the given filesystem and
// directory path (typically the objects directory, e.g. ".git/objects").
func NewFile(fs billy.Filesystem, path string) *File {
	return NewFileWithWriteMode(fs, path, FileWriteLegacy)
}

// NewFileWithWriteMode creates a File with an explicit on-disk
// write mode. Reading always supports both legacy and object-map formats.
func NewFileWithWriteMode(fs billy.Filesystem, path string, mode FileWriteMode) *File {
	return &File{
		fs:             fs,
		path:           path,
		mode:           mode,
		nativeToCompat: make(map[plumbing.Hash]plumbing.Hash),
		compatToNative: make(map[plumbing.Hash]plumbing.Hash),
	}
}

// load reads all map files into memory. Must be called with m.mu held
// (at least for writing).
func (m *File) load() error {
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

		nativeToCompat, _, err := decodeMapFile(data)
		if err != nil {
			return fmt.Errorf("decode map file %s: %w", entry.Name(), err)
		}

		for native, compat := range nativeToCompat {
			setMapping(m.nativeToCompat, m.compatToNative, native, compat)
		}
	}

	if err := m.loadLegacyTextIndex(); err != nil {
		return err
	}

	m.loaded = true
	return nil
}

// ToCompat returns the compat hash for a native hash.
func (m *File) ToCompat(native plumbing.Hash) (plumbing.Hash, error) {
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

// ToNative returns the native hash for a compat hash.
func (m *File) ToNative(compat plumbing.Hash) (plumbing.Hash, error) {
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

// Add records or replaces a native/compat hash mapping on disk.
func (m *File) Add(native, compat plumbing.Hash) error {
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
	case FileWriteLegacy:
		if err := m.writeMappings(append(sortedPairs(m.nativeToCompat), mapPair{native: native, compat: compat})); err != nil {
			return err
		}
	case FileWriteObjectMap:
		if err := m.writeObjectMap([]mapPair{{native: native, compat: compat}}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported compat mapping write mode: %d", m.mode)
	}

	setMapping(m.nativeToCompat, m.compatToNative, native, compat)
	return nil
}

func (m *File) overwriteMapping(native, compat plumbing.Hash) error {
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

	if m.mode == FileWriteObjectMap {
		if err := m.replaceObjectMapSnapshot(pairs); err != nil {
			return err
		}
		m.nativeToCompat = make(map[plumbing.Hash]plumbing.Hash, len(pairs))
		m.compatToNative = make(map[plumbing.Hash]plumbing.Hash, len(pairs))
		for _, pair := range pairs {
			setMapping(m.nativeToCompat, m.compatToNative, pair.native, pair.compat)
		}
		return nil
	}
	if err := m.writeMappings(pairs); err != nil {
		return err
	}

	setMapping(m.nativeToCompat, m.compatToNative, native, compat)
	return nil
}

// Count returns the number of persisted mappings.
func (m *File) Count() (int, error) {
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
func (m *File) Compact() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.load(); err != nil {
		return err
	}
	if len(m.nativeToCompat) == 0 {
		return nil
	}

	if m.mode == FileWriteObjectMap {
		return m.replaceObjectMapSnapshot(sortedPairs(m.nativeToCompat))
	}

	return m.writeMappings(sortedPairs(m.nativeToCompat))
}

func (m *File) writeMappings(pairs []mapPair) error {
	if len(pairs) == 0 {
		return nil
	}

	switch m.mode {
	case FileWriteLegacy:
		return m.writeLegacyTextIndex(pairs)
	case FileWriteObjectMap:
		return m.writeObjectMap(pairs)
	default:
		return fmt.Errorf("unsupported compat mapping write mode: %d", m.mode)
	}
}
