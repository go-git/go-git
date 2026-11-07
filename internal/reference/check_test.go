package reference

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage"
)

func TestCheckUnchanged(t *testing.T) {
	t.Parallel()

	const name = plumbing.ReferenceName("refs/heads/main")
	a := plumbing.NewHashReference(name, plumbing.NewHash("1111111111111111111111111111111111111111"))
	b := plumbing.NewHashReference(name, plumbing.NewHash("2222222222222222222222222222222222222222"))
	zero := plumbing.NewHashReference(name, plumbing.ZeroHash)
	symbolic := plumbing.NewSymbolicReference(name, "refs/heads/other")

	tests := []struct {
		name    string
		current *plumbing.Reference
		old     *plumbing.Reference
		want    error
	}{
		{"same hash", a, a, nil},
		{"other hash", a, b, storage.ErrReferenceHasChanged},
		{"missing", nil, a, plumbing.ErrReferenceNotFound},
		{"zero old, missing", nil, zero, nil},
		{"zero old, existing", a, zero, storage.ErrReferenceHasChanged},
		{"zero old, holding the zero hash", zero, zero, nil},
		{"zero old, symbolic", symbolic, zero, storage.ErrReferenceHasChanged},
		{"symbolic old, symbolic", symbolic, symbolic, nil},
		{"symbolic old, missing", nil, symbolic, plumbing.ErrReferenceNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.ErrorIs(t, CheckUnchanged(tc.current, tc.old), tc.want)
		})
	}
}
