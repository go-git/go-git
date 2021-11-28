package iocopy

import (
	"bytes"
	"crypto/rand"
	"io/ioutil"
	"testing"
)

func BenchmarkCopy(b *testing.B) {
	data := make([]byte, 1024*1024) // 1 MB
	rand.Read(data)

	b.ResetTimer()

	var src bytes.Reader
	for i := 0; i < b.N; i++ {
		src.Reset(data)

		Copy(ioutil.Discard, &src)
	}
}
