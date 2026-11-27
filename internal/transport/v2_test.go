package transport

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
)

func TestIsEmptyLsRefs(t *testing.T) {
	t.Parallel()

	main := plumbing.NewBranchReferenceName("main")
	head := plumbing.NewSymbolicReference(plumbing.HEAD, main)
	develop := plumbing.NewHashReference(
		plumbing.NewBranchReferenceName("develop"),
		plumbing.NewHash("6ecf0ef2c2dffb796033e5a02219af86ec6584e5"),
	)

	tests := []struct {
		name string
		out  *packp.LsRefsOutput
		want bool
	}{
		{
			name: "no references",
			out:  &packp.LsRefsOutput{},
			want: true,
		},
		{
			name: "only an unborn HEAD",
			out:  &packp.LsRefsOutput{References: []*plumbing.Reference{head}, UnbornHead: main},
			want: true,
		},
		{
			name: "unborn HEAD and a branch",
			out:  &packp.LsRefsOutput{References: []*plumbing.Reference{head, develop}, UnbornHead: main},
		},
		{
			name: "a branch",
			out:  &packp.LsRefsOutput{References: []*plumbing.Reference{develop}},
		},
		{
			name: "HEAD whose target a ref-prefix left out",
			out:  &packp.LsRefsOutput{References: []*plumbing.Reference{head}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsEmptyLsRefs(tt.out))
		})
	}
}
