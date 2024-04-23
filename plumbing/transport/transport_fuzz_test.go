package transport

import (
	"testing"
)

func FuzzNewEndpoint(f *testing.F) {
	f.Fuzz(func(_ *testing.T, input string) {
		NewEndpoint(input)
	})
}
