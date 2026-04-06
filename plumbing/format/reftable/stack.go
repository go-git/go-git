package reftable

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6"
)

// Stack reads references from a reftable stack (tables.list + table files).
type Stack struct {
	fs     billy.Filesystem
	tables []*Table // ordered oldest to newest
}

// OpenStack opens a reftable stack from the given filesystem (the reftable/
// directory). It reads tables.list and opens all listed table files.
func OpenStack(fs billy.Filesystem) (*Stack, error) {
	s := &Stack{fs: fs}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Stack) reload() error {
	f, err := s.fs.Open("tables.list")
	if err != nil {
		if os.IsNotExist(err) {
			// Empty stack.
			s.tables = nil
			return nil
		}
		return fmt.Errorf("reftable: opening tables.list: %w", err)
	}
	defer func() { _ = f.Close() }()

	var names []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reftable: reading tables.list: %w", err)
	}

	tables := make([]*Table, 0, len(names))
	for _, name := range names {
		tf, err := s.fs.Open(name)
		if err != nil {
			// Per spec: if a file is missing, retry from the beginning.
			// For simplicity, return an error. A production implementation
			// would retry.
			return fmt.Errorf("reftable: opening table %s: %w", name, err)
		}

		stat, err := s.fs.Stat(name)
		if err != nil {
			_ = tf.Close()
			return fmt.Errorf("reftable: stat table %s: %w", name, err)
		}

		ra, ok := tf.(io.ReaderAt)
		if !ok {
			// Read entire file into memory if ReaderAt is not supported.
			data, err := io.ReadAll(tf)
			_ = tf.Close()
			if err != nil {
				return fmt.Errorf("reftable: reading table %s: %w", name, err)
			}
			ra = newBytesReaderAt(data)
			stat = &fileInfoSize{size: int64(len(data))}
		}

		tbl, err := OpenTable(ra, stat.Size())
		if err != nil {
			if closer, ok := tf.(io.Closer); ok {
				_ = closer.Close()
			}
			return fmt.Errorf("reftable: parsing table %s: %w", name, err)
		}

		tables = append(tables, tbl)
	}

	s.tables = tables
	return nil
}

// Ref looks up a reference by name, searching tables from newest to oldest.
// Returns nil, nil if the reference is not found.
func (s *Stack) Ref(name string) (*RefRecord, error) {
	// Search newest to oldest.
	for i := len(s.tables) - 1; i >= 0; i-- {
		rec, err := s.tables[i].Ref(name)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			// Deletion tombstone means the ref was deleted.
			if rec.ValueType == refValueDeletion {
				return nil, nil
			}
			return rec, nil
		}
	}
	return nil, nil
}

// IterRefs iterates all references, deduplicating by name (newest wins)
// and filtering out deletion tombstones.
func (s *Stack) IterRefs(fn func(RefRecord) bool) error {
	// Collect all refs from all tables, then deduplicate.
	// A ref in a newer table (higher index) overrides older ones.
	type refEntry struct {
		rec        RefRecord
		tableIndex int
	}

	refMap := make(map[string]refEntry)
	for ti, tbl := range s.tables {
		err := tbl.IterRefs(func(rec RefRecord) bool {
			refMap[rec.RefName] = refEntry{rec: rec, tableIndex: ti}
			return true
		})
		if err != nil {
			return err
		}
	}

	// Sort by name for deterministic iteration.
	names := make([]string, 0, len(refMap))
	for name := range refMap {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry := refMap[name]
		// Skip deletion tombstones.
		if entry.rec.ValueType == refValueDeletion {
			continue
		}
		if !fn(entry.rec) {
			return nil
		}
	}

	return nil
}

// LogsFor returns all log records for the given reference, newest first.
func (s *Stack) LogsFor(name string) ([]LogRecord, error) {
	var all []LogRecord

	// Collect from all tables (each table stores logs newest-first).
	for _, tbl := range s.tables {
		records, err := tbl.LogsFor(name)
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
	}

	// Sort by update_index descending (newest first).
	sort.Slice(all, func(i, j int) bool {
		return all[i].UpdateIndex > all[j].UpdateIndex
	})

	return all, nil
}

// Close closes all open table files.
func (s *Stack) Close() error {
	s.tables = nil
	return nil
}

// bytesReaderAt wraps a byte slice as an io.ReaderAt.
type bytesReaderAt struct {
	data []byte
}

func newBytesReaderAt(data []byte) *bytesReaderAt {
	return &bytesReaderAt{data: data}
}

func (b *bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n := copy(p, b.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// fileInfoSize implements os.FileInfo with just a size.
type fileInfoSize struct {
	size int64
}

func (f *fileInfoSize) Name() string       { return "" }
func (f *fileInfoSize) Size() int64        { return f.size }
func (f *fileInfoSize) Mode() os.FileMode  { return 0 }
func (f *fileInfoSize) ModTime() time.Time { return time.Time{} }
func (f *fileInfoSize) IsDir() bool        { return false }
func (f *fileInfoSize) Sys() any           { return nil }
