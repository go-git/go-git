package reftable

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v6"
)

// Stack reads references from a reftable stack (tables.list + table files).
type Stack struct {
	fs       billy.Filesystem
	tables   []*Table // ordered oldest to newest
	mu       sync.RWMutex
	hashSize int
}

// OpenStack opens a reftable stack from the given filesystem (the reftable/
// directory). It reads tables.list and opens all listed table files.
func OpenStack(fs billy.Filesystem, hashSize int) (*Stack, error) {
	if hashSize != 20 && hashSize != 32 {
		return nil, fmt.Errorf("reftable: unsupported hash size %d", hashSize)
	}
	s := &Stack{fs: fs, hashSize: hashSize}
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
			oldTables := s.tables
			s.tables = nil
			for _, t := range oldTables {
				_ = t.Close()
			}
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

	// Create a map of existing open tables to reuse.
	existing := make(map[string]*Table)
	for _, t := range s.tables {
		existing[t.name] = t
	}

	tables := make([]*Table, 0, len(names))
	for _, name := range names {
		if t, ok := existing[name]; ok {
			tables = append(tables, t)
			delete(existing, name) // remove so we don't close it
			continue
		}

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
		if tbl.hashSize != s.hashSize {
			if closer, ok := tf.(io.Closer); ok {
				_ = closer.Close()
			}
			return fmt.Errorf("reftable: table %s hash size %d does not match stack hash size %d", name, tbl.hashSize, s.hashSize)
		}

		tbl.name = name
		tables = append(tables, tbl)
	}

	s.tables = tables
	for _, t := range existing {
		_ = t.Close()
	}
	return nil
}

// Ref looks up a reference by name, searching tables from newest to oldest.
// Returns nil, nil if the reference is not found.
func (s *Stack) Ref(name string) (*RefRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Search newest to oldest.
	for _, v := range slices.Backward(s.tables) {
		rec, err := v.Ref(name)
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.iterRefsLocked(fn)
}

func (s *Stack) iterRefsLocked(fn func(RefRecord) bool) error {
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
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	lk, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lk)

	if err := s.reload(); err != nil {
		return err
	}

	idx := s.nextUpdateIndex()
	rec.UpdateIndex = idx

	return s.writeNewTableLocked([]RefRecord{rec}, nil, idx, idx)
}

// RemoveRef removes a reference by writing a deletion tombstone.
func (s *Stack) RemoveRef(name string) error {
	lk, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lk)

	if err := s.reload(); err != nil {
		return err
	}

	idx := s.nextUpdateIndex()
	rec := RefRecord{
		RefName:     name,
		UpdateIndex: idx,
		ValueType:   refValueDeletion,
	}
	return s.writeNewTableLocked([]RefRecord{rec}, nil, idx, idx)
}

// AddLog writes a log record to the reftable stack.
func (s *Stack) AddLog(rec LogRecord) error {
	lk, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lk)

	if err := s.reload(); err != nil {
		return err
	}

	idx := s.nextUpdateIndex()
	rec.UpdateIndex = idx

	return s.writeNewTableLocked(nil, []LogRecord{rec}, idx, idx)
}

// writeNewTableLocked creates a new reftable file with the given records,
// appends it to the stack, and updates tables.list. Assumes the lock is held.
func (s *Stack) writeNewTableLocked(refs []RefRecord, logs []LogRecord, minIdx, maxIdx uint64) error {
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
		HashSize:       s.hashSize,
	})

	for i := range refs {
		w.AddRef(refs[i])
	}
	for i := range logs {
		w.AddLog(logs[i])
	}

	if err := w.Close(); err != nil {
		_ = f.Close()
		_ = s.fs.Remove(tableName)
		return fmt.Errorf("reftable: writing table: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = s.fs.Remove(tableName)
		return fmt.Errorf("reftable: closing table: %w", err)
	}

	if err := s.appendTablesList(tableName); err != nil {
		_ = s.fs.Remove(tableName)
		return err
	}

	if err := s.reload(); err != nil {
		return err
	}

	if err := s.autoCompact(); err != nil {
		return fmt.Errorf("reftable: auto-compactor failed: %w", err)
	}

	return nil
}

// appendTablesList appends a table name to the tables.list file.
func (s *Stack) appendTablesList(tableName string) error {
	// Read existing tables.list.
	var names []string
	f, err := s.fs.Open("tables.list")
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reftable: opening tables.list: %w", err)
		}
	} else {
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				names = append(names, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("reftable: reading tables.list: %w", err)
		}
	}

	names = append(names, tableName)
	return s.writeTablesListAtomic(names)
}

func (s *Stack) writeTablesListAtomic(names []string) error {
	var buf bytes.Buffer
	for _, name := range names {
		fmt.Fprintln(&buf, name)
	}

	tmpName := "tables.list.tmp"
	wf, err := s.fs.Create(tmpName)
	if err != nil {
		return fmt.Errorf("reftable: creating tables.list.tmp: %w", err)
	}
	if _, err := wf.Write(buf.Bytes()); err != nil {
		_ = wf.Close()
		_ = s.fs.Remove(tmpName)
		return fmt.Errorf("reftable: writing tables.list.tmp: %w", err)
	}
	if err := wf.Close(); err != nil {
		_ = s.fs.Remove(tmpName)
		return fmt.Errorf("reftable: closing tables.list.tmp: %w", err)
	}

	if err := s.fs.Rename(tmpName, "tables.list"); err != nil {
		_ = s.fs.Remove(tmpName)
		return fmt.Errorf("reftable: renaming tables.list.tmp to tables.list: %w", err)
	}
	return nil
}

// Compact merges all tables in the stack into a single table.
// Compact merges all tables in the stack into a single table.
func (s *Stack) Compact() error {
	lk, err := s.lock()
	if err != nil {
		return err
	}
	defer s.unlock(lk)

	if err := s.reload(); err != nil {
		return err
	}

	if len(s.tables) <= 1 {
		return nil
	}

	return s.compactRange(0, len(s.tables)-1)
}

func (s *Stack) autoCompact() error {
	if len(s.tables) <= 5 {
		return nil
	}

	sizes := make([]uint64, len(s.tables))
	for i, t := range s.tables {
		sizes[i] = uint64(t.size)
	}

	start, end := suggestCompactionSegment(sizes)
	if start >= 0 && end >= 0 {
		return s.compactRange(start, end)
	}
	return nil
}

// compactRange merges tables in the range [start, end] (inclusive) into a single table.
func (s *Stack) compactRange(start, end int) error {
	if start < 0 || end < 0 || start >= end || end >= len(s.tables) {
		return fmt.Errorf("reftable: invalid compaction range [%d, %d]", start, end)
	}

	type refEntry struct {
		rec        RefRecord
		tableIndex int
	}

	refMap := make(map[string]refEntry)
	for i := start; i <= end; i++ {
		err := s.tables[i].IterRefs(func(rec RefRecord) bool {
			refMap[rec.RefName] = refEntry{rec: rec, tableIndex: i}
			return true
		})
		if err != nil {
			return fmt.Errorf("reftable: compaction: iterating refs of table %d: %w", i, err)
		}
	}

	var refs []RefRecord
	refNames := make([]string, 0, len(refMap))
	for name := range refMap {
		refNames = append(refNames, name)
	}
	sort.Strings(refNames)
	for _, name := range refNames {
		refs = append(refs, refMap[name].rec)
	}

	type logKeyStruct struct {
		refName string
		idx     uint64
	}
	logMap := make(map[logKeyStruct]LogRecord)
	for i := start; i <= end; i++ {
		err := s.tables[i].IterLogs(func(rec LogRecord) bool {
			key := logKeyStruct{refName: rec.RefName, idx: rec.UpdateIndex}
			logMap[key] = rec
			return true
		})
		if err != nil {
			return fmt.Errorf("reftable: compaction: iterating logs of table %d: %w", i, err)
		}
	}

	var logs []LogRecord
	type logSortEntry struct {
		key logKeyStruct
		rec LogRecord
	}
	var sortedLogs []logSortEntry
	for k, v := range logMap {
		sortedLogs = append(sortedLogs, logSortEntry{key: k, rec: v})
	}
	sort.Slice(sortedLogs, func(i, j int) bool {
		if sortedLogs[i].key.refName != sortedLogs[j].key.refName {
			return sortedLogs[i].key.refName < sortedLogs[j].key.refName
		}
		return sortedLogs[i].key.idx > sortedLogs[j].key.idx
	})
	for _, entry := range sortedLogs {
		logs = append(logs, entry.rec)
	}

	minIdx := s.tables[start].footer.minUpdateIndex
	maxIdx := s.tables[end].footer.maxUpdateIndex

	tableName, err := generateTableName(minIdx, maxIdx)
	if err != nil {
		return fmt.Errorf("reftable: compaction: generating table name: %w", err)
	}

	f, err := s.fs.Create(tableName)
	if err != nil {
		return fmt.Errorf("reftable: compaction: creating table %s: %w", tableName, err)
	}

	w := NewWriter(f, WriterOptions{
		MinUpdateIndex: minIdx,
		MaxUpdateIndex: maxIdx,
		HashSize:       s.hashSize,
	})

	for i := range refs {
		w.AddRef(refs[i])
	}
	for i := range logs {
		w.AddLog(logs[i])
	}

	if err := w.Close(); err != nil {
		_ = f.Close()
		_ = s.fs.Remove(tableName)
		return fmt.Errorf("reftable: compaction: writing table: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = s.fs.Remove(tableName)
		return fmt.Errorf("reftable: compaction: closing table: %w", err)
	}

	var names []string
	tf, err := s.fs.Open("tables.list")
	if err != nil {
		_ = s.fs.Remove(tableName)
		return fmt.Errorf("reftable: compaction: opening tables.list: %w", err)
	}
	defer func() { _ = tf.Close() }()
	scanner := bufio.NewScanner(tf)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			names = append(names, line)
		}
	}
	if err := scanner.Err(); err != nil {
		_ = s.fs.Remove(tableName)
		return fmt.Errorf("reftable: compaction: reading tables.list: %w", err)
	}

	if len(names) < len(s.tables) {
		_ = s.fs.Remove(tableName)
		return fmt.Errorf("reftable: compaction: tables.list changed concurrently")
	}
	for i := start; i <= end; i++ {
		if names[i] != s.tables[i].name {
			_ = s.fs.Remove(tableName)
			return fmt.Errorf("reftable: compaction: tables.list changed concurrently at index %d", i)
		}
	}

	newNames := make([]string, 0, len(names)-(end-start))
	newNames = append(newNames, names[:start]...)
	newNames = append(newNames, tableName)
	newNames = append(newNames, names[end+1:]...)

	if err := s.writeTablesListAtomic(newNames); err != nil {
		_ = s.fs.Remove(tableName)
		return err
	}

	var toDelete []string
	for i := start; i <= end; i++ {
		toDelete = append(toDelete, s.tables[i].name)
	}

	if err := s.reload(); err != nil {
		return err
	}

	for _, name := range toDelete {
		_ = s.fs.Remove(name)
	}
	return nil
}

func suggestCompactionSegment(sizes []uint64) (int, int) {
	n := len(sizes)
	if n <= 1 {
		return -1, -1
	}

	var bytes uint64
	bytes = sizes[n-1]
	compactionStart := -1

	for i := n - 2; i >= 0; i-- {
		if sizes[i] < 2*bytes {
			compactionStart = i
			bytes += sizes[i]
		} else {
			break
		}
	}

	if compactionStart >= 0 {
		return compactionStart, n - 1
	}

	return -1, -1
}

type stackLock struct {
	f billy.File
}

func (s *Stack) lock() (*stackLock, error) {
	s.mu.Lock()

	f, err := s.fs.OpenFile("tables.list.lock", os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("reftable: opening lock file: %w", err)
	}

	if locker, ok := f.(billy.Locker); ok {
		if err := locker.Lock(); err != nil {
			_ = f.Close()
			s.mu.Unlock()
			return nil, fmt.Errorf("reftable: locking lock file: %w", err)
		}
	}
	return &stackLock{f: f}, nil
}

func (s *Stack) unlock(lk *stackLock) {
	if lk == nil {
		return
	}
	if locker, ok := lk.f.(billy.Locker); ok {
		_ = locker.Unlock()
	}
	_ = lk.f.Close()
	s.mu.Unlock()
}

// Close closes all open table files.
func (s *Stack) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tables {
		_ = t.Close()
	}
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
