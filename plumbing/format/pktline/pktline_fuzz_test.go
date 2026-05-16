package pktline

import (
	"bufio"
	"bytes"
	"io"
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

func FuzzSidebandRoundTrip(f *testing.F) {
	f.Add([]byte{}, uint8(1))
	f.Add([]byte("hello"), uint8(1))
	f.Add([]byte("progress\n"), uint8(2))
	f.Add(bytes.Repeat([]byte("a"), MaxPayloadSize+10), uint8(1))
	f.Add(bytes.Repeat([]byte("b"), DefaultSize*2), uint8(1))

	f.Fuzz(func(t *testing.T, payload []byte, b uint8) {
		band := byte(b%3) + 1
		for _, max := range []int{DefaultSize, MaxSize} {
			var buf bytes.Buffer
			if _, err := WriteSideband(&buf, band, payload, max); err != nil {
				continue
			}
			s := NewSidebandScanner(&buf, io.Discard, max)
			var got []byte
			for s.Scan() {
				got = append(got, s.Bytes()...)
			}
			if band == BandData {
				if !bytes.Equal(got, payload) {
					t.Fatalf("round-trip mismatch: got %d bytes, want %d (max=%d)", len(got), len(payload), max)
				}
			}
		}
	})
}

func FuzzSidebandReader(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("0000"))
	f.Add([]byte("0005\x01"))
	f.Add([]byte("000a\x01hello"))
	f.Add([]byte("000a\x02prog\n"))
	f.Add([]byte("000a\x03oops"))
	f.Add([]byte("0007\x02hi"))
	f.Add(append([]byte("0008\x01ab"), []byte("0000")...))

	f.Fuzz(func(_ *testing.T, data []byte) {
		for _, max := range []int{DefaultSize, MaxSize} {
			r := NewSidebandReader(bytes.NewReader(data), nil, max)
			buf := make([]byte, 64)
			for range 100 {
				_, err := r.Read(buf)
				if err != nil {
					break
				}
			}
		}
	})
}

func FuzzSidebandScanner(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("0000"))
	f.Add([]byte("0005\x01"))
	f.Add([]byte("000a\x01hello"))
	f.Add([]byte("000a\x02prog\n"))
	f.Add([]byte("000a\x03oops"))
	f.Add([]byte("0007\x02hi"))
	f.Add([]byte("0007\x07hi"))
	f.Add([]byte("0008\x02ab\r"))
	f.Add(append([]byte("0008\x01ab"), []byte("0000")...))

	f.Fuzz(func(_ *testing.T, data []byte) {
		for _, max := range []int{DefaultSize, MaxSize} {
			sc := NewSidebandScanner(bytes.NewReader(data), nil, max)
			for range 100 {
				if !sc.Scan() {
					break
				}
				_, _, _ = sc.Len(), sc.Bytes(), sc.Text()
			}
			_ = sc.Err()
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
