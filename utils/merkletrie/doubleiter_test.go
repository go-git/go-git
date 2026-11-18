package merkletrie_test

import (
	ctx "context"
	"errors"

	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type fakeNoder struct {
	name     string
	childErr error
}

func (n fakeNoder) Hash() []byte   { return []byte(n.name) }
func (n fakeNoder) Name() string   { return n.name }
func (n fakeNoder) String() string { return n.name }
func (n fakeNoder) IsDir() bool    { return true }
func (n fakeNoder) Skip() bool     { return false }

func (n fakeNoder) Children() ([]noder.Noder, error) {
	if n.childErr != nil {
		return nil, n.childErr
	}
	return noder.NoChildren, nil
}

func (n fakeNoder) NumChildren() (int, error) {
	children, err := n.Children()
	return len(children), err
}

func (s *DiffTreeSuite) TestDiffTreeWrapsRootErrors() {
	sentinel := errors.New("store cannot answer")
	hashEqual := func(_, _ noder.Hasher) bool { return false }

	tests := []struct {
		name    string
		fromErr error
		toErr   error
	}{
		{name: "from root children", fromErr: sentinel},
		{name: "to root children", toErr: sentinel},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			from := fakeNoder{name: "from", childErr: tc.fromErr}
			to := fakeNoder{name: "to", childErr: tc.toErr}

			_, err := merkletrie.DiffTreeContext(ctx.Background(), from, to, hashEqual)
			s.ErrorIs(err, sentinel)
		})
	}
}
