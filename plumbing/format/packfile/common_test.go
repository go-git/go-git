package packfile

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestEmptyUpdateObjectStorage(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sto := memory.NewStorage()

	err := UpdateObjectStorage(sto, &buf)
	assert.ErrorIs(t, err, ErrEmptyPackfile)
}

func newObject(t plumbing.ObjectType, cont []byte) plumbing.EncodedObject {
	o := plumbing.MemoryObject{}
	o.SetType(t)
	o.SetSize(int64(len(cont)))
	o.Write(cont)

	return &o
}

type piece struct {
	val   string
	times int
}

func genBytes(elements []piece) []byte {
	var result []byte
	for _, e := range elements {
		for i := 0; i < e.times; i++ {
			result = append(result, e.val...)
		}
	}

	return result
}

// packReaderWriter reads only the four signature bytes in ReadPack, as a
// writer that stops at the pack trailer leaves the rest of the stream.
type packReaderWriter struct{ wrote, readPack bool }

func (w *packReaderWriter) Write(p []byte) (int, error) { w.wrote = true; return len(p), nil }

func (w *packReaderWriter) ReadPack(r io.Reader) (int64, error) {
	w.readPack = true
	n, err := io.ReadFull(r, make([]byte, 4))
	return int64(n), err
}

func (w *packReaderWriter) Close() error { return nil }

// readerFromWriter has a ReadFrom that, like packReaderWriter's ReadPack,
// stops after the signature, to show copyPackfile does not call it directly.
type readerFromWriter struct {
	bytes.Buffer
	readFrom bool
}

func (w *readerFromWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFrom = true
	n, err := io.ReadFull(r, make([]byte, 4))
	return int64(n), err
}

func (w *readerFromWriter) Close() error { return nil }

func TestCopyPackfileReadPack(t *testing.T) {
	t.Parallel()

	// A *bufio.Reader implements WriterTo, which io.Copy would prefer, so the
	// pack would reach the writer through Write.
	src := strings.NewReader("PACK data after the pack")
	w := &packReaderWriter{}
	require.NoError(t, copyPackfile(w, bufio.NewReaderSize(src, 16)))
	assert.True(t, w.readPack)
	assert.False(t, w.wrote, "bytes went through Write")
	assert.Positive(t, src.Len(), "the stream was read to EOF")
}

// TestCopyPackfileReaderFrom checks that a writer implementing
// [io.ReaderFrom] still receives the whole stream: ReadFrom promises to read
// to EOF, so it is not taken to stop at the pack trailer.
func TestCopyPackfileReaderFrom(t *testing.T) {
	t.Parallel()

	const data = "PACK data after the pack"
	w := &readerFromWriter{}
	require.NoError(t, copyPackfile(w, strings.NewReader(data)))
	assert.False(t, w.readFrom, "ReadFrom was called directly")
	assert.Equal(t, data, w.String())
}
