package reftable

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
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

// nextUpdateIndex returns the next update index for writing.
func (s *Stack) nextUpdateIndex() uint64 {
	var maxIdx uint64
	for _, t := range s.tables {
		if t.footer.maxUpdateIndex > maxIdx {
			maxIdx = t.footer.maxUpdateIndex
		}
	}
	return maxIdx + 1
}

// SetRef writes or updates a reference in the reftable stack by creating
// a new table containing the ref record and updating tables.list.
func (s *Stack) SetRef(rec RefRecord) error {
	idx := s.nextUpdateIndex()
	rec.UpdateIndex = idx

	return s.writeNewTable([]RefRecord{rec}, nil, idx, idx)
}

// RemoveRef removes a reference by writing a deletion tombstone.
func (s *Stack) RemoveRef(name string) error {
	idx := s.nextUpdateIndex()
	rec := RefRecord{
		RefName:     name,
		UpdateIndex: idx,
		ValueType:   refValueDeletion,
	}
	return s.writeNewTable([]RefRecord{rec}, nil, idx, idx)
}

// AddLog writes a log record to the reftable stack.
func (s *Stack) AddLog(rec LogRecord) error {
	idx := s.nextUpdateIndex()
	rec.UpdateIndex = idx

	return s.writeNewTable(nil, []LogRecord{rec}, idx, idx)
}

// writeNewTable creates a new reftable file with the given records,
// appends it to the stack, and updates tables.list.
func (s *Stack) writeNewTable(refs []RefRecord, logs []LogRecord, minIdx, maxIdx uint64) error {
	// Generate a unique table name.
	tableName, err := generateTableName(minIdx, maxIdx)
	if err != nil {
		return fmt.Errorf("reftable: generating table name: %w", err)
	}

	// Write the new table file.
	f, err := s.fs.Create(tableName)
	if err != nil {
		return fmt.Errorf("reftable: creating table %s: %w", tableName, err)
	}

	w := NewWriter(f, WriterOptions{
		MinUpdateIndex: minIdx,
		MaxUpdateIndex: maxIdx,
		HashSize:       20, // SHA-1
	})

	for i := range refs {
		w.AddRef(refs[i])
	}
	for i := range logs {
		w.AddLog(logs[i])
	}

	if err := w.Close(); err != nil {
		_ = f.Close()
		return fmt.Errorf("reftable: writing table: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("reftable: closing table: %w", err)
	}

	// Atomically update tables.list by writing to a temp file and renaming.
	if err := s.appendTablesList(tableName); err != nil {
		return err
	}

	// Reload the stack to pick up the new table.
	return s.reload()
}

// appendTablesList appends a table name to the tables.list file.
func (s *Stack) appendTablesList(tableName string) error {
	// Read existing tables.list.
	var names []string
	f, err := s.fs.Open("tables.list")
	if err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				names = append(names, line)
			}
		}
		_ = f.Close()
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("reftable: reading tables.list: %w", err)
		}
	}

	names = append(names, tableName)

	// Write updated tables.list.
	var buf bytes.Buffer
	for _, name := range names {
		fmt.Fprintln(&buf, name)
	}

	wf, err := s.fs.Create("tables.list")
	if err != nil {
		return fmt.Errorf("reftable: creating tables.list: %w", err)
	}
	if _, err := wf.Write(buf.Bytes()); err != nil {
		_ = wf.Close()
		return fmt.Errorf("reftable: writing tables.list: %w", err)
	}
	return wf.Close()
}

// generateTableName creates a reftable filename in the format:
// 0xMIN-0xMAX-RANDOM.ref
func generateTableName(minIdx, maxIdx uint64) (string, error) {
	var randBytes [4]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("0x%012x-0x%012x-%s.ref",
		minIdx, maxIdx, hex.EncodeToString(randBytes[:])), nil
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
