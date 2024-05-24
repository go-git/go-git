package test

import (
	"errors"
	"fmt"

	check "gopkg.in/check.v1"
)

// This check.Checker implementation exists because there's no implementation
// in the library that compares errors using `errors.Is`. If / when the check
// library fixes https://github.com/go-check/check/issues/139, this code can
// likely be removed and replaced with the library implementation.
//
// Added in Go 1.13 [https://go.dev/blog/go1.13-errors] `errors.Is` is the
// best mechanism to use to compare errors that might be wrapped in other
// errors.
type errorIsChecker struct {
	*check.CheckerInfo
}

var ErrorIs check.Checker = errorIsChecker{
	&check.CheckerInfo{
		Name:   "ErrorIs",
		Params: []string{"obtained", "expected"},
	},
}

func (e errorIsChecker) Check(params []interface{}, names []string) (bool, string) {
	obtained, ok := params[0].(error)
	if !ok {
		return false, "obtained is not an error"
	}
	expected, ok := params[1].(error)
	if !ok {
		return false, "expected is not an error"
	}

	if !errors.Is(obtained, expected) {
		return false, fmt.Sprintf("obtained: %+v expected: %+v", obtained, expected)
	}
	return true, ""
}
