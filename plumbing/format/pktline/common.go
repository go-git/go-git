package pktline

var (
	// Empty is an empty pkt-line payload. When encoded, it will produce a
	// flush pkt.
	Empty = []byte{}

	// FlushPkt are the contents of a flush-pkt pkt-line.
	FlushPkt = []byte{'0', '0', '0', '0'}

	// DelimPkt are the contents of a delim-pkt pkt-line.
	DelimPkt = []byte{'0', '0', '0', '1'}

	// ResponseEndPkt are the contents of a response-end-pkt pkt-line.
	ResponseEndPkt = []byte{'0', '0', '0', '2'}
)
