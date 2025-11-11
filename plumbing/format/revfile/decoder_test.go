package revfile

import (
	"crypto"
	"testing"

	fixtures "github.com/go-git/go-git-fixtures/v5"
	"github.com/go-git/go-git/v6/plumbing/format/idxfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeSHA256Rev(t *testing.T) {
	fixture := fixtures.ByTag("packfile-sha256").One()
	revf := fixture.Rev()
	require.NotNil(t, revf)

	idxf := fixture.Idx()
	require.NotNil(t, idxf)

	idx := idxfile.NewMemoryIndex(crypto.SHA256.Size())
	idec := idxfile.NewDecoder(idxf)
	err := idec.Decode(idx)
	require.NoError(t, err)

	d := NewDecoder(revf)
	err = d.Decode(idx)
	assert.NoError(t, err)
}
