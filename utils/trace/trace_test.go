package trace

import (
	"bytes"
	"io"
	"log"
	"testing"
)

func TestMain(m *testing.M) {
	defer SetLogger(newLogger())
	if code := m.Run(); code != 0 {
		panic(code)
	}
}

func setUpTest(t testing.TB, buf *bytes.Buffer) {
	t.Cleanup(func() {
		if buf != nil {
			buf.Reset()
		}
		SetTarget(0)
	})
	w := io.Discard
	if buf != nil {
		w = buf
	}
	SetLogger(log.New(w, "", 0))
}

func TestEmpty(t *testing.T) {
	var buf bytes.Buffer
	setUpTest(t, &buf)
	General.Print("test")
	if buf.String() != "" {
		t.Error("expected empty string")
	}
}

func TestOneTarget(t *testing.T) {
	var buf bytes.Buffer
	setUpTest(t, &buf)
	SetTarget(General)
	General.Print("test")
	if buf.String() != "test\n" {
		t.Error("expected 'test'")
	}
}

func TestMultipleTargets(t *testing.T) {
	var buf bytes.Buffer
	setUpTest(t, &buf)
	SetTarget(General | Packet)
	General.Print("a")
	Packet.Print("b")
	if buf.String() != "a\nb\n" {
		t.Error("expected 'a\nb\n'")
	}
}

func TestPrintf(t *testing.T) {
	var buf bytes.Buffer
	setUpTest(t, &buf)
	SetTarget(General)
	General.Printf("a %d", 1)
	if buf.String() != "a 1\n" {
		t.Error("expected 'a 1\n'")
	}
}

func TestDisabledMultipleTargets(t *testing.T) {
	var buf bytes.Buffer
	setUpTest(t, &buf)
	SetTarget(General)
	General.Print("a")
	Packet.Print("b")
	if buf.String() != "a\n" {
		t.Error("expected 'a\n'")
	}
}

func BenchmarkDisabledTarget(b *testing.B) {
	setUpTest(b, nil)
	for i := 0; i < b.N; i++ {
		General.Print("test")
	}
}

func BenchmarkEnabledTarget(b *testing.B) {
	setUpTest(b, nil)
	SetTarget(General)
	for i := 0; i < b.N; i++ {
		General.Print("test")
	}
}
