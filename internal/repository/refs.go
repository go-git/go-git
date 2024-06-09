package repository

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

func ExpandRef(s storer.ReferenceStorer, ref plumbing.ReferenceName) (*plumbing.Reference, error) {
	// For improving troubleshooting, this preserves the error for the provided `ref`,
	// and returns the error for that specific ref in case all parse rules fails.
	var ret error
	for _, rule := range plumbing.RefRevParseRules {
		resolvedRef, err := storer.ResolveReference(s, plumbing.ReferenceName(fmt.Sprintf(rule, ref)))

		if err == nil {
			return resolvedRef, nil
		} else if ret == nil {
			ret = err
		}
	}

	return nil, ret
}
