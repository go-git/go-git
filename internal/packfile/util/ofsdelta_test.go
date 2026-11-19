package util_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	packutil "github.com/go-git/go-git/v6/internal/packfile/util"
)

func TestValidOFSDeltaBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		deltaOffset    int64
		negativeOffset int64
		want           bool
	}{
		{name: "earlier entry", deltaOffset: 100, negativeOffset: 88, want: true},
		{name: "one byte back", deltaOffset: 100, negativeOffset: 1, want: true},
		{name: "zero offset points at itself", deltaOffset: 100, negativeOffset: 0, want: false},
		{name: "negative offset points forward", deltaOffset: 100, negativeOffset: -1, want: false},
		{name: "base at pack start", deltaOffset: 100, negativeOffset: 100, want: false},
		{name: "base before pack start", deltaOffset: 100, negativeOffset: 101, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, packutil.ValidOFSDeltaBase(tc.deltaOffset, tc.negativeOffset))
		})
	}
}
