package pktline

const (
	// Err is returned when the pktline has encountered an error.
	Err = iota - 1

	// Flush is the numeric value of a flush packet. It is returned when the
	// pktline is a flush packet.
	Flush

	// Delim is the numeric value of a delim packet. It is returned when the
	// pktline is a delim packet.
	Delim

	// ResponseEnd is the numeric value of a response-end packet. It is
	// returned when the pktline is a response-end packet.
	ResponseEnd
)

var (
	// Empty is an empty pkt-line payload.
	Empty = []byte{}

	// FlushPkt are the contents of a flush-pkt pkt-line.
	FlushPkt = []byte{'0', '0', '0', '0'}

	// DelimPkt are the contents of a delim-pkt pkt-line.
	DelimPkt = []byte{'0', '0', '0', '1'}

	// ResponseEndPkt are the contents of a response-end-pkt pkt-line.
	ResponseEndPkt = []byte{'0', '0', '0', '2'}

	// emptyPkt is an empty string pkt-line payload.
	emptyPkt = []byte{'0', '0', '0', '4'}
)
