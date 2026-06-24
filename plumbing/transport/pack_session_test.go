package transport

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing"
)

func TestNewRemoteRefs(t *testing.T) {
	t.Parallel()

	head := plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5")

	tests := []struct {
		name       string
		refs       []*plumbing.Reference
		wantUnborn plumbing.ReferenceName
	}{
		{
			name:       "no refs",
			refs:       nil,
			wantUnborn: "",
		},
		{
			name: "hash refs only",
			refs: []*plumbing.Reference{
				plumbing.NewHashReference("refs/heads/main", head),
			},
			wantUnborn: "",
		},
		{
			name: "symbolic head with present target is not unborn",
			refs: []*plumbing.Reference{
				plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main"),
				plumbing.NewHashReference("refs/heads/main", head),
			},
			wantUnborn: "",
		},
		{
			name: "symbolic head with absent target is unborn",
			refs: []*plumbing.Reference{
				plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main"),
			},
			wantUnborn: "refs/heads/main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := NewRemoteRefs(tt.refs)
			assert.Equal(t, tt.refs, rr.References)
			assert.Equal(t, tt.wantUnborn, rr.Unborn)
		})
	}
}
