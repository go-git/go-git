package packfile

import "github.com/go-git/go-git/v6/plumbing"

// StatusObserver forwards packfile parsing progress to a StatusChan.
type StatusObserver struct {
	statusChan plumbing.StatusChan
	update     plumbing.StatusUpdate
}

// NewStatusObserver returns an observer that sends packfile parsing progress
// to statusChan. A nil statusChan makes the observer a no-op.
func NewStatusObserver(statusChan plumbing.StatusChan) *StatusObserver {
	return &StatusObserver{statusChan: statusChan}
}

// OnHeader reports the number of objects declared by the packfile header.
func (o *StatusObserver) OnHeader(count uint32) error {
	o.update = plumbing.StatusUpdate{
		Stage:        plumbing.StatusFetch,
		ObjectsTotal: int(count),
	}
	o.statusChan.SendUpdate(o.update)
	return nil
}

// OnInflatedObjectHeader reports progress before advancing the completed
// object count. The footer reports the sole completion update for the stage.
func (o *StatusObserver) OnInflatedObjectHeader(plumbing.ObjectType, int64, int64) error {
	o.statusChan.SendUpdate(o.update)
	o.update.ObjectsDone++
	return nil
}

// OnInflatedObjectContent is a no-op.
func (*StatusObserver) OnInflatedObjectContent(plumbing.Hash, int64, uint32, []byte) error {
	return nil
}

// OnFooter reports the final packfile parsing progress.
func (o *StatusObserver) OnFooter(plumbing.Hash) error {
	o.statusChan.SendUpdate(o.update)
	return nil
}
