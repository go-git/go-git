//go:build unix

package pack_test

import (
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/stretchr/testify/suite"
)

func TestPackScanner(t *testing.T) {
	fixture := fixtures.NewOSFixture(fixtures.Basic().One(), t.TempDir())
	suite.Run(t, &PackHandlerSuite[uint64]{
		newPackHandler: func() packHandler[uint64] {
			pack, idx, rev := fixture.Packfile(), fixture.Idx(), fixture.Rev()
			t.Cleanup(func() {
				pack.Close()
				idx.Close()
				rev.Close()
			})
			return newPackScanner(pack, idx, rev)
		},
	})
}
