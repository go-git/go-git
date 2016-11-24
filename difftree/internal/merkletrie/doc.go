package merkletrie

/*
Package merkletrie gives support for n-ary trees that are at the same
time Merkle trees and Radix trees, and provides an efficient tree
comparison algorithm for them.

Git trees are Radix n-ary trees in virtue of the names of their
tree entries.  At the same time, git trees are Merkle trees thanks to
their hashes.

When comparing git trees, the simple approach of alphabetically sorting
their elements and comparing the resulting lists is not enough as it
depends linearly on the number of files in the trees: When a directory
has lots of files but none of them has been modified, this approach is
very expensive.  We can do better by prunning whole directories that
have not change, by just by looking at their hashes.  This package
provides the tools to do exactly that.

This package defines Radix-Merkle trees as nodes that should have:
- a hash: the Merkle part of the Radix-Merkle tree
- a key: the Radix part of the Radix-Merkle tree

The Merkle hash condition is not enforced by this package though.  This
means that node hashes doesn't have to take into account the hashes of
their children,  which is good for testing purposes.

Nodes in the Radix-Merkle tree are abstracted by the Noder interface.
The intended use is that git.Tree implements this interface.
*/
