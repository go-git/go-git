package sync

import (
	"io"
	"strings"
	"testing"
)

func TestGetAndPutBufioReader(t *testing.T) {
	wanted := "someinput"
	r := GetBufioReader(strings.NewReader(wanted))
	if r == nil {
		t.Error("nil was not expected")
	}

	got, err := r.ReadString(0)
	if err != nil && err != io.EOF {
		t.Errorf("unexpected error reading string: %v", err)
	}

	if wanted != got {
		t.Errorf("wanted %q got %q", wanted, got)
	}

	PutBufioReader(r)
}
