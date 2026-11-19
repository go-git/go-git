package transport

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadFinished(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{
			name: "success on a live context",
			ctx:  context.Background(),
			err:  nil,
			want: true,
		},
		{
			name: "ordinary error on a live context",
			ctx:  context.Background(),
			err:  io.ErrUnexpectedEOF,
			want: true,
		},
		{
			// A deadline belonging to something else. The read finished, so
			// the reader has to be closed.
			name: "foreign deadline on a live context",
			ctx:  context.Background(),
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "cancellation from this context",
			ctx:  cancelled,
			err:  context.Canceled,
			want: false,
		},
		{
			name: "wrapped cancellation from this context",
			ctx:  cancelled,
			err:  fmt.Errorf("negotiate: %w", context.Canceled),
			want: false,
		},
		{
			// A deadline reported by a context that is done is taken for
			// that context's own, whoever it belonged to. The answer is
			// inexact in this one direction, and errs towards not closing.
			name: "deadline on a cancelled context",
			ctx:  cancelled,
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			// Cancelled just after a read that had already finished.
			name: "ordinary error on a cancelled context",
			ctx:  cancelled,
			err:  io.ErrUnexpectedEOF,
			want: true,
		},
		{
			name: "success on a cancelled context",
			ctx:  cancelled,
			err:  nil,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ReadFinished(tt.ctx, tt.err))
		})
	}
}
