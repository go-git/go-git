package oidmap

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-billy/v6"

	"github.com/go-git/go-git/v6/plumbing"
)

const legacyLooseObjectIdxFile = "loose-object-idx"

func (m *File) writeLegacyTextIndex(pairs []mapPair) error {
	if err := removeExistingMapFiles(m.fs, m.mapDir()); err != nil {
		return err
	}

	var buf bytes.Buffer
	for _, pair := range pairs {
		if _, err := fmt.Fprintf(&buf, "%s %s\n", pair.native, pair.compat); err != nil {
			return fmt.Errorf("write legacy loose-object-idx: %w", err)
		}
	}

	return atomicWriteFile(m.fs, m.legacyIdxPath(), buf.Bytes(), 0o644)
}

func (m *File) loadLegacyTextIndex() error {
	data, err := readFile(m.fs, m.legacyIdxPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read legacy loose-object-idx: %w", err)
	}

	for line := range strings.SplitSeq(string(data), "\n") {
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
		setMapping(m.nativeToCompat, m.compatToNative, native, compat)
	}
	return nil
}

func removeLegacyTextIndex(fs billy.Filesystem, path string) error {
	if err := fs.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy loose-object-idx: %w", err)
	}
	return nil
}
