package hash

import (
	"sort"
	"strings"
)

// HashesSort sorts a slice of Hashes in increasing order.
func Sort(a []ObjectID) {
	sort.Sort(ObjectIDs(a))
}

// HashSlice attaches the methods of sort.Interface to []Hash, sorting in
// increasing order.
type ObjectIDs []ObjectID

func (p ObjectIDs) Len() int           { return len(p) }
func (p ObjectIDs) Less(i, j int) bool { return strings.Compare(p[i].String(), p[j].String()) < 0 }
func (p ObjectIDs) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
