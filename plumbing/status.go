package plumbing

// StatusStage identifies a stage of packfile processing.
type StatusStage int

const (
	// StatusCount counts objects selected by a revision walk.
	StatusCount StatusStage = iota
	// StatusRead reads objects from storage for pack generation.
	StatusRead
	// StatusFixChains fixes or breaks existing delta chains.
	StatusFixChains
	// StatusSort sorts objects before delta selection.
	StatusSort
	// StatusDelta selects delta bases for objects.
	StatusDelta
	// StatusSend writes objects to an outgoing packfile.
	StatusSend
	// StatusFetch reads objects from an incoming packfile.
	StatusFetch
	// StatusIndexHash writes object hashes to a pack index.
	StatusIndexHash
	// StatusIndexCRC writes object CRC values to a pack index.
	StatusIndexCRC
	// StatusIndexOffset writes object offsets to a pack index.
	StatusIndexOffset
	// StatusDone is reserved for an overall packfile completion update.
	StatusDone

	// StatusUnknown indicates an unknown packfile processing stage.
	StatusUnknown StatusStage = -1
)

// StatusUpdate reports object-level progress for a packfile processing stage.
type StatusUpdate struct {
	Stage StatusStage

	ObjectsTotal int
	ObjectsDone  int
}

// StatusChan receives structured packfile progress updates. Callers must drain
// a non-nil channel concurrently until the operation returns: stage-start and
// stage-completion updates are delivered synchronously.
type StatusChan chan<- StatusUpdate

// SendUpdate sends update to sc. A nil StatusChan is a no-op.
func (sc StatusChan) SendUpdate(update StatusUpdate) {
	if sc == nil {
		return
	}
	sc <- update
}

// SendUpdateIfPossible sends update without blocking, except that completion
// updates are always delivered. A nil StatusChan is a no-op.
func (sc StatusChan) SendUpdateIfPossible(update StatusUpdate) {
	if sc == nil {
		return
	}
	if update.ObjectsDone == update.ObjectsTotal {
		sc <- update
		return
	}

	select {
	case sc <- update:
	default:
	}
}
