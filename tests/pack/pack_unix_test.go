//go:build unix

package pack_test

import (
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestPackScanner(t *testing.T) {
	t.Parallel()
	fixture := fixtures.NewOSFixture(fixtures.Basic().One(), t.TempDir())
	suite.Run(t, &PackHandlerSuite[uint64]{
		newPackHandler: func() packHandler[uint64] {
			pack, err := fixture.Packfile()
			require.NoError(t, err)
			idx, err := fixture.Idx()
			require.NoError(t, err)
			rev, err := fixture.Rev()
			require.NoError(t, err)
			t.Cleanup(func() {
				pack.Close()
				idx.Close()
				rev.Close()
			})
			return newPackScanner(pack, idx, rev)
		},
	})
}
