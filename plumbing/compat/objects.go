package compat

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// TranslateStoredObjects iterates over all objects in the given storage and
// computes compat hash mappings for any objects that don't already have one.
// Objects are processed in topological order: blobs first, then trees, then
// commits and tags.
//
// This is useful after a batch import (e.g. initial clone) to populate
// the mapping table for all stored objects.
func TranslateStoredObjects(s storer.EncodedObjectStorer, t *Translator) error {
	// Phase 1: Translate blobs (no dependencies).
	if err := translateObjectsOfType(s, t, plumbing.BlobObject); err != nil {
		return fmt.Errorf("translate blobs: %w", err)
	}

	// Phase 2: Translate trees (depend on blobs and other trees).
	// Trees may reference other trees, so we iterate until no new
	// translations are made.
	if err := translateObjectsWithRetry(s, t, plumbing.TreeObject); err != nil {
		return fmt.Errorf("translate trees: %w", err)
	}

	// Phase 3: Translate commits (depend on trees and other commits).
	if err := translateObjectsWithRetry(s, t, plumbing.CommitObject); err != nil {
		return fmt.Errorf("translate commits: %w", err)
	}

	// Phase 4: Translate tags (depend on any object type, including other tags).
	if err := translateObjectsWithRetry(s, t, plumbing.TagObject); err != nil {
		return fmt.Errorf("translate tags: %w", err)
	}

	return nil
}

// translateObjectsOfType translates all objects of the given type that
// don't already have a compat mapping.
func translateObjectsOfType(s storer.EncodedObjectStorer, t *Translator, objType plumbing.ObjectType) error {
	iter, err := s.IterEncodedObjects(objType)
	if err != nil {
		return err
	}
	defer iter.Close()

	return iter.ForEach(func(obj plumbing.EncodedObject) error {
		// Skip if already translated.
		if _, err := t.mapping.ToCompat(obj.Hash()); err == nil {
			return nil
		}

		_, err := t.TranslateObject(obj)
		return err
	})
}

// translateObjectsWithRetry translates objects that may have inter-type
// dependencies (e.g. trees referencing other trees, commits referencing
// other commits). It retries until a full pass adds no new translations.
func translateObjectsWithRetry(s storer.EncodedObjectStorer, t *Translator, objType plumbing.ObjectType) error {
	for {
		translated := 0
		skipped := 0

		iter, err := s.IterEncodedObjects(objType)
		if err != nil {
			return err
		}

		err = iter.ForEach(func(obj plumbing.EncodedObject) error {
			// Skip if already translated.
			if _, err := t.mapping.ToCompat(obj.Hash()); err == nil {
				return nil
			}

			_, err := t.TranslateObject(obj)
			if err != nil {
				if !errors.Is(err, plumbing.ErrObjectNotFound) {
					return err
				}

				// Dependencies not yet translated; skip for now.
				skipped++
				return nil
			}
			translated++
			return nil
		})
		iter.Close()

		if err != nil {
			return err
		}

		// If nothing was translated and nothing was skipped, we're done.
		if translated == 0 && skipped == 0 {
			return nil
		}

		// If nothing was translated but some were skipped, we have
		// unresolvable dependencies.
		if translated == 0 && skipped > 0 {
			return fmt.Errorf("unable to translate %d %s objects: missing dependencies", skipped, objType)
		}

		// If everything was translated, we're done.
		if skipped == 0 {
			return nil
		}

		// Otherwise, retry to catch objects whose deps were just translated.
	}
}
