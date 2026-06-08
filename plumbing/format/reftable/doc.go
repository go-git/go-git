// Package reftable implements reading and writing for the reftable binary
// reference storage format.
//
// The reftable format stores Git references and reflogs in a compact binary
// format that supports O(log n) lookups and efficient iteration. It is
// specified at https://git-scm.com/docs/reftable.
//
// A repository using the reftable backend stores its data in a reftable/
// directory containing a tables.list file and one or more table files
// (with a .ref extension) that contain both references and reflogs.
// The tables.list file lists all active tables from oldest
// to newest. References are looked up by searching tables in reverse
// order (newest first), with the first match winning.
package reftable
