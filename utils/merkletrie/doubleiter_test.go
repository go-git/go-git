package merkletrie_test

import (
	ctx "context"
	"errors"

	"github.com/go-git/go-git/v6/utils/merkletrie"
	"github.com/go-git/go-git/v6/utils/merkletrie/noder"
)

type fakeNoder struct {
	name     string
	children []noder.Noder
	childErr error
	numErr   error
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
	return n.children, nil
}

func (n fakeNoder) NumChildren() (int, error) {
	if n.numErr != nil {
		return 0, n.numErr
	}
	return len(n.children), nil
}

func (s *DiffTreeSuite) TestDiffTreeWrapsRootErrors() {
	const (
		fromName      = "from"
		toName        = "to"
		directoryName = "d"
	)

	sentinel := errors.New("store cannot answer")
	hashEqual := func(_, _ noder.Hasher) bool { return false }

	tests := []struct {
		name     string
		fromErr  error
		toErr    error
		children bool
	}{
		{name: "from root children", fromErr: sentinel},
		{name: "to root children", toErr: sentinel},
		{name: "from directory child count", fromErr: sentinel, children: true},
		{name: "to directory child count", toErr: sentinel, children: true},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			var from, to noder.Noder
			if tc.children {
				from = fakeNoder{name: fromName, children: []noder.Noder{
					fakeNoder{name: directoryName, numErr: tc.fromErr},
				}}
				to = fakeNoder{name: toName, children: []noder.Noder{
					fakeNoder{name: directoryName, numErr: tc.toErr},
				}}
			} else {
				from = fakeNoder{name: fromName, childErr: tc.fromErr}
				to = fakeNoder{name: toName, childErr: tc.toErr}
			}

			_, err := merkletrie.DiffTreeContext(ctx.Background(), from, to, hashEqual)
			s.ErrorIs(err, sentinel)
		})
	}
}
