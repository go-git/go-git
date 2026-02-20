//go:build windows
// +build windows

package transport

import (
	"testing"
)

func TestNewEndpoint_WindowsDrivePath_IsFile(t *testing.T) {
	// Paths like "C:/path/to/repo" or "C:\\path\\to\\repo" should
	// be treated as local file endpoints on Windows, not SCP/SSH.
	eps := []string{
		"C:/Users/Administrator/Downloads/workdir/repository",
		"C:\\Users\\Administrator\\Downloads\\workdir\\repository",
	}

	for _, ep := range eps {
		e, err := NewEndpoint(ep)
		if err != nil {
			t.Fatalf("NewEndpoint(%q) returned error: %v", ep, err)
		}
		if e.Protocol != "file" {
			t.Fatalf("expected protocol=file for %q, got %q", ep, e.Protocol)
		}
		if e.Path != ep {
			t.Fatalf("expected path=%q for %q, got %q", ep, ep, e.Path)
		}
	}
}
