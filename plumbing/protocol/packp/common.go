package packp

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
	eq  = []byte{'='}

	// advertised-refs
	null       = []byte("\x00")
	peeled     = []byte("^{}")
	noHeadMark = []byte(" capabilities^{}\x00")

	// upload-request
	want            = []byte("want ")
	shallow         = []byte("shallow ")
	deepen          = []byte("deepen")
	deepenCommits   = []byte("deepen ")
	deepenSince     = []byte("deepen-since ")
	deepenReference = []byte("deepen-not ")

	// shallow-update
	unshallow = []byte("unshallow ")

	// server-response
	ack = []byte("ACK")
	nak = []byte("NAK")

	// updreq
	shallowNoSp = []byte("shallow")
)

func isFlush(payload []byte) bool {
	return len(payload) == 0
}
