package reftable

import (
	"io"
	"testing"

	"github.com/go-git/go-billy/v6"
	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openFixtureTable opens the first .ref file from the reftable fixture's
// reftable/ directory.
func openFixtureTable(t *testing.T) *Table {
	t.Helper()
	f := fixtures.ByTag("reftable").One()
	dotgit, err := f.DotGit()
	require.NoError(t, err)

	reftableDir, err := dotgit.Chroot("reftable")
	require.NoError(t, err)

	refFile := findRefFile(t, reftableDir)
	rf, err := reftableDir.Open(refFile)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rf.Close() })

	data, err := io.ReadAll(rf)
	require.NoError(t, err)

	tbl, err := OpenTable(newBytesReaderAt(data), int64(len(data)))
	require.NoError(t, err)
	return tbl
}

// findRefFile returns the name of the first .ref file in a reftable directory.
func findRefFile(t *testing.T, fs billy.Filesystem) string {
	t.Helper()
	entries, err := fs.ReadDir(".")
	require.NoError(t, err)
	for _, e := range entries {
		name := e.Name()
		if len(name) > 4 && name[len(name)-4:] == ".ref" {
			return name
		}
	}
	t.Fatal("no .ref file found in reftable directory")
	return ""
}

func TestTableFooter(t *testing.T) {
	t.Parallel()
	tbl := openFixtureTable(t)

	assert.Equal(t, versionV1, tbl.footer.version)
	assert.Equal(t, 20, tbl.hashSize)
}

func TestTableRef(t *testing.T) {
	t.Parallel()
	tbl := openFixtureTable(t)

	// HEAD should exist as a symbolic ref.
	head, err := tbl.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, head, "HEAD not found")
	assert.Equal(t, uint8(refValueSymref), head.ValueType)

	// refs/heads/master or refs/heads/main should exist.
	var mainRef *RefRecord
	for _, name := range []string{"refs/heads/master", "refs/heads/main"} {
		rec, err := tbl.Ref(name)
		require.NoError(t, err)
		if rec != nil {
			mainRef = rec
			break
		}
	}
	require.NotNil(t, mainRef, "no main/master branch found")
	assert.Equal(t, uint8(refValueVal1), mainRef.ValueType)
	assert.Len(t, mainRef.Value, 20)
}

func TestTableRefNotFound(t *testing.T) {
	t.Parallel()
	tbl := openFixtureTable(t)

	rec, err := tbl.Ref("refs/heads/nonexistent")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestTableIterRefs(t *testing.T) {
	t.Parallel()
	tbl := openFixtureTable(t)

	var names []string
	err := tbl.IterRefs(func(rec RefRecord) bool {
		names = append(names, rec.RefName)
		return true
	})
	require.NoError(t, err)

	assert.Contains(t, names, "HEAD")
	assert.Greater(t, len(names), 1, "expected more than just HEAD")
}

func TestTableRefHEADSymbolic(t *testing.T) {
	t.Parallel()
	tbl := openFixtureTable(t)

	rec, err := tbl.Ref("HEAD")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, uint8(refValueSymref), rec.ValueType)
	assert.NotEmpty(t, rec.Target)
}

func TestTableIterLogs(t *testing.T) {
	t.Parallel()
	tbl := openFixtureTable(t)

	var entries []LogRecord
	err := tbl.IterLogs(func(rec LogRecord) bool {
		entries = append(entries, rec)
		return true
	})
	require.NoError(t, err)

	// The fixture may or may not have log entries. If it does, verify basics.
	for _, e := range entries {
		assert.NotEmpty(t, e.RefName, "log entry should have a ref name")
	}
}

