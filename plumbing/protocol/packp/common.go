package packp

import "bytes"

type stateFn func() stateFn

const (
	// common
	hashSize = 40

	// advrefs
	head   = "HEAD"
	noHead = "capabilities^{}"
)

var (
	// common
	sp  = []byte(" ")
	eol = []byte("\n")

	// advrefs
	null       = []byte("\x00")
	peeled     = []byte("^{}")
	noHeadMark = []byte(" capabilities^{}\x00")

	// ulreq
	want            = []byte("want ")
	shallow         = []byte("shallow ")
	deepen          = []byte("deepen")
	deepenCommits   = []byte("deepen ")
	deepenSince     = []byte("deepen-since ")
	deepenReference = []byte("deepen-not ")
)

// Capabilities are a single string or a name=value.
// Even though we are only going to read at moust 1 value, we return
// a slice of values, as Capability.Add receives that.
func readCapability(data []byte) (name string, values []string) {
	pair := bytes.SplitN(data, []byte{'='}, 2)
	if len(pair) == 2 {
		values = append(values, string(pair[1]))
	}

	return string(pair[0]), values
}
