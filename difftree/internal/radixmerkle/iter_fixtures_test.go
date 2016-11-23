package merkletrie

// this files contains fixtures for testing the Iter.
//
// - iter... functions returns iterators for newly created trees
//   for example:
//
//   + iterLeaf returns an iterator for simple tree with just the root.
//
//   + iter2Horizontal returns an iterator for a tree with 2 nodes, both
//     childs of the root.
//
// - runs... contains sets of tests, indexed by a string that helps
//   to understand each test: "nsn" means next, then step, then next
//   again.  The test also contains the expected keys of the nodes you
//   will get when calling the operations over the correspoding trees:
//   Example: runs2HorizontalSorted with iter2HorizontalSorted and so on.

func iterLeaf() *Iter {
	root := newNode(hash, "root", empty)
	return NewIter(root)
}

var runs0 = map[string][]test{
	"nn": {{next, ""}, {next, ""}},
	"ns": {{next, ""}, {step, ""}},
	"sn": {{step, ""}, {next, ""}},
	"ss": {{step, ""}, {step, ""}},
}

//   root
//    |
//    a
func iter1() *Iter {
	a := newNode(hash, "a", empty)
	root := newNode(hash, "root", []*node{a})
	return NewIter(root)
}

var runs1 = map[string][]test{
	"nn": {{next, "a"}, {next, ""}},
	"ns": {{next, "a"}, {step, ""}},
	"sn": {{step, "a"}, {next, ""}},
	"ss": {{step, "a"}, {step, ""}},
}

//     root
//      / \
//     a   b
func iter2HorizontalSorted() *Iter {
	a := newNode(hash, "a", empty)
	b := newNode(hash, "b", empty)
	root := newNode(hash, "root", []*node{a, b})
	return NewIter(root)
}

//     root
//      / \
//     b   a
func iter2HorizontalReverse() *Iter {
	a := newNode(hash, "a", empty)
	b := newNode(hash, "b", empty)
	root := newNode(hash, "root", []*node{b, a})
	return NewIter(root)
}

var runs2Horizontal = map[string][]test{
	"nnn": {{next, "a"}, {next, "b"}, {next, ""}},
	"nns": {{next, "a"}, {next, "b"}, {step, ""}},
	"nsn": {{next, "a"}, {step, "b"}, {next, ""}},
	"nss": {{next, "a"}, {step, "b"}, {step, ""}},
	"snn": {{step, "a"}, {next, "b"}, {next, ""}},
	"sns": {{step, "a"}, {next, "b"}, {step, ""}},
	"ssn": {{step, "a"}, {step, "b"}, {next, ""}},
	"sss": {{step, "a"}, {step, "b"}, {step, ""}},
}

//     root
//      |
//      a
//      |
//      b
func iter2VerticalSorted() *Iter {
	b := newNode(hash, "b", empty)
	a := newNode(hash, "a", []*node{b})
	root := newNode(hash, "root", []*node{a})
	return NewIter(root)
}

var runs2VerticalSorted = map[string][]test{
	"nnn": {{next, "a"}, {next, ""}, {next, ""}},
	"nns": {{next, "a"}, {next, ""}, {step, ""}},
	"nsn": {{next, "a"}, {step, "b"}, {next, ""}},
	"nss": {{next, "a"}, {step, "b"}, {step, ""}},
	"snn": {{step, "a"}, {next, ""}, {next, ""}},
	"sns": {{step, "a"}, {next, ""}, {step, ""}},
	"ssn": {{step, "a"}, {step, "b"}, {next, ""}},
	"sss": {{step, "a"}, {step, "b"}, {step, ""}},
}

//     root
//      |
//      b
//      |
//      a
func iter2VerticalReverse() *Iter {
	a := newNode(hash, "a", empty)
	b := newNode(hash, "b", []*node{a})
	root := newNode(hash, "root", []*node{b})
	return NewIter(root)
}

var runs2VerticalReverse = map[string][]test{
	"nnn": {{next, "b"}, {next, ""}, {next, ""}},
	"nns": {{next, "b"}, {next, ""}, {step, ""}},
	"nsn": {{next, "b"}, {step, "a"}, {next, ""}},
	"nss": {{next, "b"}, {step, "a"}, {step, ""}},
	"snn": {{step, "b"}, {next, ""}, {next, ""}},
	"sns": {{step, "b"}, {next, ""}, {step, ""}},
	"ssn": {{step, "b"}, {step, "a"}, {next, ""}},
	"sss": {{step, "b"}, {step, "a"}, {step, ""}},
}

//     root
//      /|\
//     c a b
func iter3Horizontal() *Iter {
	a := newNode(hash, "a", empty)
	b := newNode(hash, "b", empty)
	c := newNode(hash, "c", empty)
	root := newNode(hash, "root", []*node{c, a, b})
	return NewIter(root)
}

var runs3Horizontal = map[string][]test{
	"nnnn": {{next, "a"}, {next, "b"}, {next, "c"}, {next, ""}},
	"nnns": {{next, "a"}, {next, "b"}, {next, "c"}, {step, ""}},
	"nnsn": {{next, "a"}, {next, "b"}, {step, "c"}, {next, ""}},
	"nnss": {{next, "a"}, {next, "b"}, {step, "c"}, {step, ""}},
	"nsnn": {{next, "a"}, {step, "b"}, {next, "c"}, {next, ""}},
	"nsns": {{next, "a"}, {step, "b"}, {next, "c"}, {step, ""}},
	"nssn": {{next, "a"}, {step, "b"}, {step, "c"}, {next, ""}},
	"nsss": {{next, "a"}, {step, "b"}, {step, "c"}, {step, ""}},
	"snnn": {{step, "a"}, {next, "b"}, {next, "c"}, {next, ""}},
	"snns": {{step, "a"}, {next, "b"}, {next, "c"}, {step, ""}},
	"snsn": {{step, "a"}, {next, "b"}, {step, "c"}, {next, ""}},
	"snss": {{step, "a"}, {next, "b"}, {step, "c"}, {step, ""}},
	"ssnn": {{step, "a"}, {step, "b"}, {next, "c"}, {next, ""}},
	"ssns": {{step, "a"}, {step, "b"}, {next, "c"}, {step, ""}},
	"sssn": {{step, "a"}, {step, "b"}, {step, "c"}, {next, ""}},
	"ssss": {{step, "a"}, {step, "b"}, {step, "c"}, {step, ""}},
}

//     root
//      |
//      b
//      |
//      c
//      |
//      a
func iter3Vertical() *Iter {
	a := newNode(hash, "a", empty)
	c := newNode(hash, "c", []*node{a})
	b := newNode(hash, "b", []*node{c})
	root := newNode(hash, "root", []*node{b})
	return NewIter(root)
}

var runs3Vertical = map[string][]test{
	"nnnn": {{next, "b"}, {next, ""}, {next, ""}, {next, ""}},
	"nnns": {{next, "b"}, {next, ""}, {next, ""}, {step, ""}},
	"nnsn": {{next, "b"}, {next, ""}, {step, ""}, {next, ""}},
	"nnss": {{next, "b"}, {next, ""}, {step, ""}, {step, ""}},
	"nsnn": {{next, "b"}, {step, "c"}, {next, ""}, {next, ""}},
	"nsns": {{next, "b"}, {step, "c"}, {next, ""}, {step, ""}},
	"nssn": {{next, "b"}, {step, "c"}, {step, "a"}, {next, ""}},
	"nsss": {{next, "b"}, {step, "c"}, {step, "a"}, {step, ""}},
	"snnn": {{step, "b"}, {next, ""}, {next, ""}, {next, ""}},
	"snns": {{step, "b"}, {next, ""}, {next, ""}, {step, ""}},
	"snsn": {{step, "b"}, {next, ""}, {step, ""}, {next, ""}},
	"snss": {{step, "b"}, {next, ""}, {step, ""}, {step, ""}},
	"ssnn": {{step, "b"}, {step, "c"}, {next, ""}, {next, ""}},
	"ssns": {{step, "b"}, {step, "c"}, {next, ""}, {step, ""}},
	"sssn": {{step, "b"}, {step, "c"}, {step, "a"}, {next, ""}},
	"ssss": {{step, "b"}, {step, "c"}, {step, "a"}, {step, ""}},
}

//     root
//      / \
//     c   a
//     |
//     b
func iter3Mix1() *Iter {
	a := newNode(hash, "a", empty)
	b := newNode(hash, "b", empty)
	c := newNode(hash, "c", []*node{b})
	root := newNode(hash, "root", []*node{c, a})
	return NewIter(root)
}

var runs3Mix1 = map[string][]test{
	"nnnn": {{next, "a"}, {next, "c"}, {next, ""}, {next, ""}},
	"nnns": {{next, "a"}, {next, "c"}, {next, ""}, {step, ""}},
	"nnsn": {{next, "a"}, {next, "c"}, {step, "b"}, {next, ""}},
	"nnss": {{next, "a"}, {next, "c"}, {step, "b"}, {step, ""}},
	"nsnn": {{next, "a"}, {step, "c"}, {next, ""}, {next, ""}},
	"nsns": {{next, "a"}, {step, "c"}, {next, ""}, {step, ""}},
	"nssn": {{next, "a"}, {step, "c"}, {step, "b"}, {next, ""}},
	"nsss": {{next, "a"}, {step, "c"}, {step, "b"}, {step, ""}},
	"snnn": {{step, "a"}, {next, "c"}, {next, ""}, {next, ""}},
	"snns": {{step, "a"}, {next, "c"}, {next, ""}, {step, ""}},
	"snsn": {{step, "a"}, {next, "c"}, {step, "b"}, {next, ""}},
	"snss": {{step, "a"}, {next, "c"}, {step, "b"}, {step, ""}},
	"ssnn": {{step, "a"}, {step, "c"}, {next, ""}, {next, ""}},
	"ssns": {{step, "a"}, {step, "c"}, {next, ""}, {step, ""}},
	"sssn": {{step, "a"}, {step, "c"}, {step, "b"}, {next, ""}},
	"ssss": {{step, "a"}, {step, "c"}, {step, "b"}, {step, ""}},
}

//     root
//      / \
//     b   a
//         |
//         c
func iter3Mix2() *Iter {
	b := newNode(hash, "b", empty)
	c := newNode(hash, "c", empty)
	a := newNode(hash, "a", []*node{c})
	root := newNode(hash, "root", []*node{b, a})
	return NewIter(root)
}

var runs3Mix2 = map[string][]test{
	"nnnn": {{next, "a"}, {next, "b"}, {next, ""}, {next, ""}},
	"nnns": {{next, "a"}, {next, "b"}, {next, ""}, {step, ""}},
	"nnsn": {{next, "a"}, {next, "b"}, {step, ""}, {next, ""}},
	"nnss": {{next, "a"}, {next, "b"}, {step, ""}, {step, ""}},
	"nsnn": {{next, "a"}, {step, "c"}, {next, "b"}, {next, ""}},
	"nsns": {{next, "a"}, {step, "c"}, {next, "b"}, {step, ""}},
	"nssn": {{next, "a"}, {step, "c"}, {step, "b"}, {next, ""}},
	"nsss": {{next, "a"}, {step, "c"}, {step, "b"}, {step, ""}},
	"snnn": {{step, "a"}, {next, "b"}, {next, ""}, {next, ""}},
	"snns": {{step, "a"}, {next, "b"}, {next, ""}, {step, ""}},
	"snsn": {{step, "a"}, {next, "b"}, {step, ""}, {next, ""}},
	"snss": {{step, "a"}, {next, "b"}, {step, ""}, {step, ""}},
	"ssnn": {{step, "a"}, {step, "c"}, {next, "b"}, {next, ""}},
	"ssns": {{step, "a"}, {step, "c"}, {next, "b"}, {step, ""}},
	"sssn": {{step, "a"}, {step, "c"}, {step, "b"}, {next, ""}},
	"ssss": {{step, "a"}, {step, "c"}, {step, "b"}, {step, ""}},
}

//      root
//      / | \
//     /  |  ----
//    f   d      h --------
//   /\         /  \      |
//  e   a      j   b      g
//  |  / \     |
//  l  n  k    icm
//     |
//     o
//     |
//     p
func iterCrazy() *Iter {
	l := newNode(hash, "l", empty)
	e := newNode(hash, "e", []*node{l})

	p := newNode(hash, "p", empty)
	o := newNode(hash, "o", []*node{p})
	n := newNode(hash, "n", []*node{o})
	k := newNode(hash, "k", empty)
	a := newNode(hash, "a", []*node{n, k})
	f := newNode(hash, "f", []*node{e, a})

	d := newNode(hash, "d", empty)

	i := newNode(hash, "i", empty)
	c := newNode(hash, "c", empty)
	m := newNode(hash, "m", empty)
	j := newNode(hash, "j", []*node{i, c, m})
	b := newNode(hash, "b", empty)
	g := newNode(hash, "g", empty)
	h := newNode(hash, "h", []*node{j, b, g})

	root := newNode(hash, "root", []*node{f, d, h})
	return NewIter(root)
}

var (
	n = next
	s = step
)

var runsCrazy = map[string][]test{
	"nn nn n": {{n, "d"}, {n, "f"}, {n, "h"}, {n, ""}, {n, ""}},
	"nn nn s": {{n, "d"}, {n, "f"}, {n, "h"}, {n, ""}, {s, ""}},
	"nn ns n": {{n, "d"}, {n, "f"}, {n, "h"}, {s, "b"}, {n, "g"}},
	"nn ns s": {{n, "d"}, {n, "f"}, {n, "h"}, {s, "b"}, {s, "g"}},
	"nn sn n": {{n, "d"}, {n, "f"}, {s, "a"}, {n, "e"}, {n, "h"}},
	"nn sn s": {{n, "d"}, {n, "f"}, {s, "a"}, {n, "e"}, {s, "l"}},
	"nn ss n": {{n, "d"}, {n, "f"}, {s, "a"}, {s, "k"}, {n, "n"}},
	"nn ss s": {{n, "d"}, {n, "f"}, {s, "a"}, {s, "k"}, {s, "n"}},
	"ns nn n": {{n, "d"}, {s, "f"}, {n, "h"}, {n, ""}, {n, ""}},
	"ns nn s": {{n, "d"}, {s, "f"}, {n, "h"}, {n, ""}, {s, ""}},
	"ns ns n": {{n, "d"}, {s, "f"}, {n, "h"}, {s, "b"}, {n, "g"}},
	"ns ns s": {{n, "d"}, {s, "f"}, {n, "h"}, {s, "b"}, {s, "g"}},
	"ns sn n": {{n, "d"}, {s, "f"}, {s, "a"}, {n, "e"}, {n, "h"}},

	"ns ss ns ss": {
		{n, "d"}, {s, "f"},
		{s, "a"}, {s, "k"},
		{n, "n"}, {s, "o"},
		{s, "p"}, {s, "e"},
	},

	"ns ss ns sn": {
		{n, "d"}, {s, "f"},
		{s, "a"}, {s, "k"},
		{n, "n"}, {s, "o"},
		{s, "p"}, {n, "e"},
	},

	"nn ns ns ss nn": {
		{n, "d"}, {n, "f"},
		{n, "h"}, {s, "b"},
		{n, "g"}, {s, "j"},
		{s, "c"}, {s, "i"},
		{n, "m"}, {n, ""},
	},
}
