package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/packfile"
	"github.com/go-git/go-git/v6/plumbing/revlist"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

type pipelinedOptions struct {
	PackWindow           uint
	SkipDeltaCompression bool
	LoaderCount          int
}

// writePipelinedPack enumerates wants/haves through revlist.Stream (or a
// supplied PackObjectWalker), loads objects in parallel through a worker
// pool, then hands the resulting hash slice to packfile.Encoder.
//
// w is the data sink (already sideband-muxed by the caller when sideband
// is negotiated). progress, when non-nil, owns sideband progress emission
// for this call. The "Counting objects" / "Writing objects" messages are
// emitted only on phase boundaries; per-object streaming counters are a
// follow-up.
func writePipelinedPack(
	ctx context.Context,
	w io.Writer,
	st storer.EncodedObjectStorer,
	wants, haves []plumbing.Hash,
	opts pipelinedOptions,
	progress *progressWriter,
) error {
	if opts.LoaderCount <= 0 {
		opts.LoaderCount = runtime.GOMAXPROCS(0)
	}

	// Use a cancellable context so we can abort loaders when one fails.
	// internalCancel tracks whether we triggered the cancellation ourselves
	// (due to a loader error); if so, we suppress the resulting context.Canceled
	// from the enumerator since the real error is in loaderErrs.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	loaderCancelled := false

	entries, enumErrCh := enumerateEntries(ctx, st, wants, haves)

	// loaded carries objects from loader pool to the collector.
	loaded := make(chan plumbing.EncodedObject, 64)

	var loaderErrs []error
	var loaderMu sync.Mutex
	var lwg sync.WaitGroup

	for i := 0; i < opts.LoaderCount; i++ {
		lwg.Add(1)
		go func() {
			defer lwg.Done()
			for {
				select {
				case e, ok := <-entries:
					if !ok {
						return
					}
					obj, err := loadObject(st, e.Hash, opts.SkipDeltaCompression)
					if err != nil {
						loaderMu.Lock()
						loaderErrs = append(loaderErrs, fmt.Errorf("load %s: %w", e.Hash, err))
						loaderCancelled = true
						loaderMu.Unlock()
						// Cancel context so enumeration and sibling loaders stop.
						cancel()
						return
					}
					select {
					case loaded <- obj:
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Close loaded once all loaders have exited.
	go func() {
		lwg.Wait()
		close(loaded)
	}()

	var hashes []plumbing.Hash
	for obj := range loaded {
		hashes = append(hashes, obj.Hash())
	}

	if err := <-enumErrCh; err != nil {
		loaderMu.Lock()
		internal := loaderCancelled
		loaderMu.Unlock()
		if !internal {
			// External cancellation or genuine enumeration error.
			return fmt.Errorf("enumerate: %w", err)
		}
		// Loader error triggered ctx cancel; real error is in loaderErrs below.
	}
	if len(loaderErrs) > 0 {
		return errors.Join(loaderErrs...)
	}

	if progress != nil {
		progress.Flush("Counting objects: %d, done.", len(hashes))
	}

	enc := packfile.NewEncoder(w, st, false)
	if _, err := enc.Encode(hashes, opts.PackWindow); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if progress != nil {
		progress.Flush("Writing objects: 100%% (%d/%d), done.", len(hashes), len(hashes))
	}
	return nil
}

// enumerateEntries starts an enumeration goroutine and returns a channel of
// entries and a channel that receives the enumeration error (or nil). The
// entries channel is always closed before the error is sent.
//
// When st implements storer.PackObjectWalker, PackObjects is used instead of
// revlist.Stream.
func enumerateEntries(
	ctx context.Context,
	st storer.EncodedObjectStorer,
	wants, haves []plumbing.Hash,
) (<-chan revlist.Entry, <-chan error) {
	entries := make(chan revlist.Entry, 64)
	errCh := make(chan error, 1)

	if walker, ok := st.(storer.PackObjectWalker); ok {
		go func() {
			hashes, err := walker.PackObjects(ctx, wants, haves)
			if err != nil {
				close(entries)
				errCh <- err
				return
			}
			for _, h := range hashes {
				select {
				case entries <- revlist.Entry{Hash: h, Type: plumbing.AnyObject}:
				case <-ctx.Done():
					close(entries)
					errCh <- ctx.Err()
					return
				}
			}
			close(entries)
			errCh <- nil
		}()
		return entries, errCh
	}

	// revlist.Stream closes the channel itself (defer close(out)).
	go func() {
		_, err := revlist.Stream(ctx, st, wants, haves, entries)
		errCh <- err
	}()
	return entries, errCh
}

// loadObject loads a single object from st. When skipDelta is false and st
// implements storer.DeltaObjectStorer, it prefers the delta representation.
func loadObject(st storer.EncodedObjectStorer, h plumbing.Hash, skipDelta bool) (plumbing.EncodedObject, error) {
	if !skipDelta {
		if d, ok := st.(storer.DeltaObjectStorer); ok {
			return d.DeltaObject(plumbing.AnyObject, h)
		}
	}
	return st.EncodedObject(plumbing.AnyObject, h)
}
