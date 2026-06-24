package pktline

import (
	"bufio"
	"bytes"
	"testing"
)

func FuzzRead(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("0000"))
	f.Add([]byte("0001"))
	f.Add([]byte("0002"))
	f.Add([]byte("0003"))
	f.Add([]byte("0004"))
	f.Add([]byte("0005a"))
	f.Add([]byte("0008ERR "))
	f.Add([]byte("000cERR EOF\n"))
	f.Add([]byte("fff1"))
	f.Add([]byte("0008XRR 0005E"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		for _, size := range []int{0, 1, LenSize - 1, LenSize, MaxSize} {
			buf := make([]byte, size)
			if len(buf) >= LenSize+1+len("RR ") {
				copy(buf[LenSize+1:], "RR ")
			}
			_, _ = Read(bytes.NewReader(data), buf)
		}
	})
}

func FuzzPeekLine(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("0000"))
	f.Add([]byte("0001"))
	f.Add([]byte("0002"))
	f.Add([]byte("0003"))
	f.Add([]byte("0004"))
	f.Add([]byte("0005a"))
	f.Add([]byte("0008ERR "))
	f.Add([]byte("000cERR EOF\n"))
	f.Add([]byte("fff1"))
	f.Add([]byte("0008XRR 0005E"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _, _ = PeekLine(bufio.NewReader(bytes.NewReader(data)))
	})
}

func FuzzReadLine(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("0000"))
	f.Add([]byte("0001"))
	f.Add([]byte("0002"))
	f.Add([]byte("0003"))
	f.Add([]byte("0004"))
	f.Add([]byte("0005a"))
	f.Add([]byte("0008ERR "))
	f.Add([]byte("000cERR EOF\n"))
	f.Add([]byte("fff1"))
	f.Add([]byte("0008XRR 0005E"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		r := bytes.NewReader(data)
		for {
			before := r.Len()
			_, _, err := ReadLine(r)
			if err != nil || r.Len() == 0 || r.Len() == before {
				break
			}
		}
	})
}

func FuzzScanner(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("0000"))
	f.Add([]byte("0001"))
	f.Add([]byte("0002"))
	f.Add([]byte("0003"))
	f.Add([]byte("0004"))
	f.Add([]byte("0005a"))
	f.Add([]byte("0008ERR "))
	f.Add([]byte("000cERR EOF\n"))
	f.Add([]byte("fff1"))
	f.Add([]byte("0008XRR 0005E"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		sc := NewScanner(bytes.NewReader(data))
		for range 100 {
			if !sc.Scan() {
				break
			}
			_, _, _ = sc.Len(), sc.Bytes(), sc.Text()
		}
		_ = sc.Err()
	})
}
