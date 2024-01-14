package packp

import (
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFilterBlobNone(t *testing.T) {
	require.EqualValues(t, "blob:none", FilterBlobNone())
}

func TestFilterBlobLimit(t *testing.T) {
	require.EqualValues(t, "blob:limit=0", FilterBlobLimit(0))
	require.EqualValues(t, "blob:limit=1000", FilterBlobLimit(1000))
}

func TestFilterTreeDepth(t *testing.T) {
	require.EqualValues(t, "tree:0", FilterTreeDepth(0))
	require.EqualValues(t, "tree:1", FilterTreeDepth(1))
	require.EqualValues(t, "tree:2", FilterTreeDepth(2))
}

func TestFilterObjectType(t *testing.T) {
	require.EqualValues(t, "object:type=tag", FilterObjectType(plumbing.TagObject))
	require.EqualValues(t, "object:type=commit", FilterObjectType(plumbing.CommitObject))
	require.EqualValues(t, "object:type=tree", FilterObjectType(plumbing.TreeObject))
	require.EqualValues(t, "object:type=blob", FilterObjectType(plumbing.BlobObject))
}

func TestFilterCombine(t *testing.T) {
	require.EqualValues(t, "combine:tree%3A2+blob%3Anone",
		FilterCombine(
			FilterTreeDepth(2),
			FilterBlobNone(),
		),
	)
}
