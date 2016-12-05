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

	// updreq
	shallowNoSp = []byte("shallow")
)
