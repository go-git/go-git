package sync

import (
	"bytes"
	"runtime"
	"testing"
	"time"
)

func TestPutBufioReaderReleasesReader(t *testing.T) {
	t.Parallel()

	released := make(chan struct{})
	src := bytes.NewReader(make([]byte, 64))
	runtime.AddCleanup(src, func(ch chan struct{}) { close(ch) }, released)

	br := GetBufioReader(src)
	PutBufioReader(br)

	runtime.GC()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("pooled bufio.Reader still references the reader it wrapped")
	}
	runtime.KeepAlive(br)
}
