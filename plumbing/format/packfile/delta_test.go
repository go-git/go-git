package packfile

import (
	"fmt"

	. "gopkg.in/check.v1"
)

type DeltaSuite struct {
	testCases []deltaTest
}

var _ = Suite(&DeltaSuite{})

type deltaTest struct {
	description string
	base        []piece
	target      []piece
}

func (s *DeltaSuite) SetUpSuite(c *C) {
	s.testCases = []deltaTest{{
		description: "distinct file",
		base:        []piece{{"0", 300}},
		target:      []piece{{"2", 200}},
	}, {
		description: "same file",
		base:        []piece{{"1", 3000}},
		target:      []piece{{"1", 3000}},
	}, {
		description: "small file",
		base:        []piece{{"1", 3}},
		target:      []piece{{"1", 3}, {"0", 1}},
	}, {
		description: "big file",
		base:        []piece{{"1", 300000}},
		target:      []piece{{"1", 30000}, {"0", 1000000}},
	}, {
		description: "add elements before",
		base:        []piece{{"0", 200}},
		target:      []piece{{"1", 300}, {"0", 200}},
	}, {
		description: "add 10 times more elements at the end",
		base:        []piece{{"1", 300}, {"0", 200}},
		target:      []piece{{"0", 2000}},
	}, {
		description: "add elements between",
		base:        []piece{{"0", 400}},
		target:      []piece{{"0", 200}, {"1", 200}, {"0", 200}},
	}, {
		description: "add elements after",
		base:        []piece{{"0", 200}},
		target:      []piece{{"0", 200}, {"1", 200}},
	}, {
		description: "modify elements at the end",
		base:        []piece{{"1", 300}, {"0", 200}},
		target:      []piece{{"0", 100}},
	}, {
		description: "complex modification",
		base: []piece{{"0", 3}, {"1", 40}, {"2", 30}, {"3", 2},
			{"4", 400}, {"5", 23}},
		target: []piece{{"1", 30}, {"2", 20}, {"7", 40}, {"4", 400},
			{"5", 10}},
	}}
}

func (s *DeltaSuite) TestAddDelta(c *C) {
	for _, t := range s.testCases {
		baseBuf := genBytes(t.base)
		targetBuf := genBytes(t.target)
		delta := DiffDelta(baseBuf, targetBuf)
		result := PatchDelta(baseBuf, delta)

		c.Log(fmt.Printf("Executing test case: %s\n", t.description))
		c.Assert(result, DeepEquals, targetBuf)
	}
}
